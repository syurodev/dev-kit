import { Events } from "@wailsio/runtime";
import { callBridge, callBridgeCancellable, type CancellableCall } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type { CodeGraphEntryPoint, CodeGraphFileEntry } from "./codegraph";

/**
 * Code Graph over a workspace's folders — the by-identity half of the bridge.
 *
 * Every call names its owner as (workspaceId, folderId). The folder's path
 * never crosses this boundary toward Go: it resolves membership itself, so a
 * save into a folder that left the workspace is refused instead of written, and
 * two folders holding the same relative path are never confused.
 * docs/design/codegraph-workspace.md § 6 lists the contract.
 */

export type FolderState = {
  folderId: string;
  accessible: boolean;
  error: string;
  isRepo: boolean;
  branch: string;
  /** Changed files per git; -1 when git did not answer in time. */
  changed: number;
};

export function listFolderStates(workspaceId: string): Promise<FolderState[]> {
  return callBridge(async () =>
    ((await AppService.WorkspaceFolderStates(workspaceId)) ?? []).map((s) => ({
      folderId: s.folderId,
      accessible: s.accessible,
      error: s.error ?? "",
      isRepo: s.isRepo,
      branch: s.branch,
      changed: s.changed,
    })),
  );
}

export type FolderFiles = { entries: CodeGraphFileEntry[]; truncated: boolean };

export function listFolderFiles(workspaceId: string, folderId: string): Promise<FolderFiles> {
  return callBridge(async () => {
    const list = await AppService.WorkspaceFileList(workspaceId, folderId);
    return {
      entries: (list.entries ?? []).map((e) => ({ path: e.path, isDir: e.isDir })),
      truncated: list.truncated,
    };
  });
}

/** What was read, and the revision a later save is checked against. */
export type FileRead = { content: string; revision: string; size: number; modifiedAt: number };

export function readFolderFile(workspaceId: string, folderId: string, path: string): Promise<FileRead> {
  return callBridge(async () => {
    const r = await AppService.WorkspaceFileRead(workspaceId, folderId, path);
    return { content: r.content, revision: r.revision, size: Number(r.size), modifiedAt: Number(r.modifiedAt) };
  });
}

export type FileStatus = {
  exists: boolean;
  revision: string;
  size: number;
  modifiedAt: number;
  /** "binary" | "too_large" | "not_regular" when the editor cannot open it as text. */
  unreadable: string;
};

export function statFolderFile(workspaceId: string, folderId: string, path: string): Promise<FileStatus> {
  return callBridge(async () => {
    const r = await AppService.WorkspaceFileStat(workspaceId, folderId, path);
    return { exists: r.exists, revision: r.revision, size: Number(r.size), modifiedAt: Number(r.modifiedAt), unreadable: r.unreadable ?? "" };
  });
}

/** The revision of a file that must not exist yet, and of "overwrite whatever is there". */
export const ABSENT_REVISION = "absent";
export const ANY_REVISION = "*";

export function writeFolderFile(
  workspaceId: string,
  folderId: string,
  path: string,
  content: string,
  expectedRevision: string,
): Promise<{ revision: string; modifiedAt: number }> {
  return callBridge(async () => {
    const r = await AppService.WorkspaceFileWrite(workspaceId, folderId, path, content, expectedRevision);
    return { revision: r.revision, modifiedAt: Number(r.modifiedAt) };
  });
}

/**
 * PDFs of a folder, drawn by Go (internal/pdfdoc). The viewer names the file by
 * (workspace, folder, path) like everything above and gets pictures back — a PDF
 * never reaches the webview as anything it could run.
 */
export type PdfInfo = {
  /** Names the bytes shown; later calls carry it so a replaced file is noticed. */
  revision: string;
  pages: number;
  /** Width and height per page in points (1/72 in), rotation applied. */
  pageSizes: Array<[number, number]>;
  size: number;
  modifiedAt: number;
};

export function pdfInfo(workspaceId: string, folderId: string, path: string): Promise<PdfInfo> {
  return callBridge(async () => {
    const r = await AppService.WorkspacePdfInfo(workspaceId, folderId, path);
    return {
      revision: r.revision,
      pages: r.pages,
      pageSizes: (r.pageSizes ?? []).map((s) => [s[0], s[1]] as [number, number]),
      size: Number(r.size),
      modifiedAt: Number(r.modifiedAt),
    };
  });
}

/** A drawn page as an `<img src>`; Go sends the bytes base64-encoded. */
export type PdfPageImage = { page: number; src: string; width: number; height: number };

export function pdfPage(
  workspaceId: string,
  folderId: string,
  path: string,
  revision: string,
  page: number,
  dpi: number,
): CancellableCall<PdfPageImage> {
  return callBridgeCancellable(
    () => AppService.WorkspacePdfPage(workspaceId, folderId, path, revision, page, dpi),
    (r) => ({ page: r.page, src: `data:${r.mimeType};base64,${r.data ?? ""}`, width: r.width, height: r.height }),
  );
}

export type PdfSearchResult = {
  hits: Array<{ page: number; snippet: string }>;
  truncated: boolean;
  /** False when the search ran out of time before every page was looked at. */
  complete: boolean;
  /** Pages with no text layer (scans): not searchable. */
  pagesWithoutText: number[];
};

export function pdfSearch(workspaceId: string, folderId: string, path: string, revision: string, query: string): CancellableCall<PdfSearchResult> {
  return callBridgeCancellable(
    () => AppService.WorkspacePdfSearch(workspaceId, folderId, path, revision, query),
    (r) => ({
      hits: (r.hits ?? []).map((h) => ({ page: h.page, snippet: h.snippet })),
      truncated: r.truncated,
      complete: r.complete,
      pagesWithoutText: r.pagesWithoutText ?? [],
    }),
  );
}

export type CodeGraphGitMark = "M" | "A" | "D" | "R";

export function folderGitStatus(workspaceId: string, folderId: string): Promise<Record<string, CodeGraphGitMark>> {
  return callBridge(async () => ((await AppService.WorkspaceGitStatus(workspaceId, folderId)) ?? {}) as Record<string, CodeGraphGitMark>);
}

/** How a folder's index stands, the words the UI uses. */
export type EntryViewState = "indexed" | "indexing" | "not-indexed" | "unsupported" | "failed";

export type FolderEntryPoints = {
  entryPointSupport?: string;
  entryPointWarnings?: string[];
  state: EntryViewState;
  entries: CodeGraphEntryPoint[];
  /** Unix ms of the index being shown; 0 when unknown. */
  indexedAt: number;
  /** Shown from the stored index while a refresh runs. */
  stale: boolean;
  error: string;
  missingToolchain: boolean;
};

export function folderEntryPoints(workspaceId: string, folderId: string): Promise<FolderEntryPoints> {
  return callBridge(async () => {
    const v = await AppService.WorkspaceEntryPoints(workspaceId, folderId);
    return {
      state: v.state as EntryViewState,
      entries: (v.entries ?? []).map((e) => ({
        id: e.id,
        verb: e.verb,
        target: e.target,
        entryNode: e.entryNode,
        kind: e.kind || "http",
        service: e.service,
        owner: e.owner,
        group: e.group,
        order: e.order,
        routeId: e.routeId,
        topic: e.topic,
        consumerGroup: e.consumerGroup,
      })),
      indexedAt: Number(v.indexedAt ?? 0),
      stale: v.stale ?? false,
      error: v.error ?? "",
      missingToolchain: v.missingToolchain ?? false,
      entryPointSupport: v.entryPointSupport ?? "",
      entryPointWarnings: v.entryPointWarnings ?? [],
    };
  });
}

export type CodeGraphOutlineSymbol = {
  id: string;
  /** Fully qualified, as the index stores it; shorten for display. */
  label: string;
  kind: string;
  line: number;
  endLine: number;
  children?: CodeGraphOutlineSymbol[];
};

export type FileOutline = {
  path: string;
  state: EntryViewState;
  symbols: CodeGraphOutlineSymbol[];
  /** Unix ms of the index the outline was read from. */
  indexedAt: number;
};

type RawSymbol = { id: string; label: string; kind: string; line: number; endLine: number; children?: RawSymbol[] | null };
const toSymbol = (s: RawSymbol): CodeGraphOutlineSymbol => ({
  id: s.id,
  label: s.label,
  kind: s.kind,
  line: s.line,
  endLine: s.endLine,
  ...(s.children?.length ? { children: s.children.map(toSymbol) } : {}),
});

export function folderFileOutline(workspaceId: string, folderId: string, path: string): Promise<FileOutline> {
  return callBridge(async () => {
    const o = await AppService.WorkspaceFileOutline(workspaceId, folderId, path);
    return {
      path: o.path,
      state: o.state as EntryViewState,
      symbols: (o.symbols ?? []).map((s) => toSymbol(s as RawSymbol)),
      indexedAt: Number(o.indexedAt ?? 0),
    };
  });
}

// ── Search ──────────────────────────────────────────────────────────────────

export type SearchRequest = {
  requestId: string;
  workspaceId: string;
  /** Empty means every folder of the workspace. */
  folderIds: string[];
  query: string;
  regex: boolean;
  caseSensitive: boolean;
  wholeWord: boolean;
  include: string[];
  exclude: string[];
  maxMatches?: number;
};

export type SearchMatch = {
  line: number;
  /** 1-based, in UTF-16 code units. */
  column: number;
  length: number;
  preview: string;
  /** UTF-16 offset in the line where `preview` begins. */
  previewStart: number;
};

export type SearchFileResult = { folderId: string; path: string; matches: SearchMatch[]; truncated: boolean };

export type SearchFolderStatus = {
  folderId: string;
  state: "ok" | "unavailable" | "error" | "duplicate";
  error: string;
  source: string;
  files: number;
  duplicateOf: string;
};

export type SearchResult = {
  files: SearchFileResult[];
  folders: SearchFolderStatus[];
  matches: number;
  filesSearched: number;
  filesSkipped: number;
  /** "" for a complete answer, else "matches" | "files". */
  truncated: string;
  elapsedMs: number;
};

/**
 * Runs a search. The returned promise has `cancel()`, which cancels the Go
 * side's context too — a superseded search stops reading files instead of only
 * being ignored.
 */
export function searchWorkspace(req: SearchRequest): CancellableCall<SearchResult> {
  return callBridgeCancellable(
    () =>
      AppService.WorkspaceSearch({
        requestId: req.requestId,
        workspaceId: req.workspaceId,
        folderIds: req.folderIds,
        query: req.query,
        regex: req.regex,
        caseSensitive: req.caseSensitive,
        wholeWord: req.wholeWord,
        include: req.include,
        exclude: req.exclude,
        maxMatches: req.maxMatches ?? 0,
      }),
    (r) => ({
      files: (r.files ?? []).map((f) => ({
        folderId: f.folderId,
        path: f.path,
        matches: (f.matches ?? []).map((m) => ({ ...m })),
        truncated: f.truncated ?? false,
      })),
      folders: (r.folders ?? []).map((f) => ({
        folderId: f.folderId,
        state: f.state as SearchFolderStatus["state"],
        error: f.error ?? "",
        source: f.source ?? "",
        files: f.files,
        duplicateOf: f.duplicateOf ?? "",
      })),
      matches: r.matches,
      filesSearched: r.filesSearched,
      filesSkipped: r.filesSkipped,
      truncated: r.truncated ?? "",
      elapsedMs: Number(r.elapsedMs ?? 0),
    }),
  );
}

export function cancelWorkspaceSearch(requestId: string): Promise<void> {
  return callBridge(() => AppService.WorkspaceSearchCancel(requestId) as Promise<void>);
}

// ── Workspace actions ───────────────────────────────────────────────────────

/** Makes a folder the primary. Only the workspace's root changes; nothing re-indexes. */
export function setWorkspacePrimary(workspaceId: string, folderId: string): Promise<void> {
  return callBridge(async () => {
    await AppService.WorkspaceSetPrimary(workspaceId, folderId);
  });
}

/** Queues folders (all when empty) to be analysed again; `cancel` withdraws them. Returns at once. */
export function reindexWorkspaceFolders(workspaceId: string, folderIds: string[], cancel = false): Promise<void> {
  return callBridge(() => AppService.WorkspaceReindex(workspaceId, folderIds, cancel) as Promise<void>);
}

// ── Open documents ──────────────────────────────────────────────────────────

export type WatchedDocument = { workspaceId: string; folderId: string; path: string };

/** Replaces the watched set; resolves to the keys that could not be watched. */
export function watchDocuments(docs: WatchedDocument[]): Promise<string[]> {
  return callBridge(async () => (await AppService.WorkspaceWatchDocuments(docs)) ?? []);
}

export type FileChangeEvent = { keys: string[]; resync: boolean };

/** Documents that may have changed on disk; `resync` means events were lost, so look at all of them. */
export function onWorkspaceFileChanged(handler: (event: FileChangeEvent) => void): () => void {
  return Events.On("workspace:fileChanged", (ev) => {
    const data = (ev.data ?? {}) as { keys?: string[]; resync?: boolean };
    handler({ keys: data.keys ?? [], resync: Boolean(data.resync) });
  });
}

/** Writes text to a file chosen in the native save dialog. "" means cancelled. */
export function saveDraftCopy(suggestedName: string, content: string): Promise<string> {
  return callBridge(() => AppService.WorkspaceSaveDraftCopy(suggestedName, content));
}

// ── Plan, by folder ─────────────────────────────────────────────────────────

export {
  getFolderPlanProgress,
  getFolderPlanEdges,
  approveFolderPlan,
  relinkFolderPlanNode,
  listFolderPlanComments,
  commentFolderPlanNode,
  countFolderPlanComments,
} from "./workspacePlan";

// ── Symbols ─────────────────────────────────────────────────────────────────

export type FolderSymbols = {
  folderId: string;
  state: EntryViewState;
  matches: Array<{ nodeId: string; label: string; kind: string; layer: string; filePath: string; line: number }>;
};

/** Classes and methods matching `query` in the indexes of the folders (all when empty); opens no project. */
export function searchWorkspaceSymbols(workspaceId: string, folderIds: string[], query: string): Promise<FolderSymbols[]> {
  return callBridge(async () =>
    ((await AppService.WorkspaceSearchSymbols(workspaceId, folderIds, query)) ?? []).map((f) => ({
      folderId: f.folderId,
      state: f.state as EntryViewState,
      matches: (f.matches ?? []).map((m) => ({ nodeId: m.nodeId, label: m.label, kind: m.kind, layer: m.layer, filePath: m.filePath, line: m.line })),
    })),
  );
}
