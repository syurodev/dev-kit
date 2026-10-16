import { fileIconComponent } from "@/components/file-icon/FileIcon";
import { CodeXml, SquareFunction, Workflow } from "lucide-react";
import { rank } from "@/workbench/palette/match";
import type { CodeGraphEntryPoint, CodeGraphSymbolMatch } from "@/bridge/codegraph";
import type { PaletteSearchGroup } from "../registry";
import { symbolDisplay } from "./sidebar/sidebarModel";

const LIMIT = 8;
/** A query shorter than this is too cheap a hit rate for a round trip to Go. */
const MIN_SYMBOL_QUERY = 2;

function fileName(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

function directoryOf(path: string): string {
  const at = path.lastIndexOf("/");
  return at === -1 ? "" : path.slice(0, at);
}

/** A thing found, with the folder it is in: nothing in the palette is a bare path or a bare id. */
export type PaletteEntry = { folderId: string; folderLabel: string; ep: CodeGraphEntryPoint };
export type PaletteFile = { folderId: string; folderLabel: string; path: string };
export type PaletteSymbol = { folderId: string; folderLabel: string; match: CodeGraphSymbolMatch };

export type CodeGraphPaletteData = {
  entryPoints: PaletteEntry[];
  files: PaletteFile[];
  /** Only ever non-empty when the query passed MIN_SYMBOL_QUERY. */
  symbols: PaletteSymbol[];
  /** More than one folder is searched, so results name theirs. */
  multi?: boolean;
};

export type CodeGraphPaletteActions = {
  openEntryPoint: (folderId: string, entry: CodeGraphEntryPoint) => void;
  openFile: (folderId: string, path: string) => void;
  openSymbol: (folderId: string, match: CodeGraphSymbolMatch) => void;
};

/**
 * The pure half: entry points, files and symbols already fetched, turned into
 * groups for `query`. Nothing is offered for an empty query — a workspace's file
 * lists alone can run into the tens of thousands, and that is not a shortlist.
 * With several folders in view each result says which one it is in, so the same
 * file name in two folders reads as two files.
 */
export function buildGroups(
  query: string,
  data: CodeGraphPaletteData,
  actions: CodeGraphPaletteActions,
): PaletteSearchGroup[] {
  const trimmed = query.trim();
  if (!trimmed) return [];

  const groups: PaletteSearchGroup[] = [];
  const where = (label: string, detail: string) => (data.multi ? (detail ? `${label} › ${detail}` : label) : detail);

  const entryPoints = rank(
    trimmed,
    data.entryPoints,
    (e) => ({ title: `${e.ep.verb} ${e.ep.target}`, others: [e.ep.target, e.ep.group] }),
    LIMIT,
  );
  if (entryPoints.length > 0) {
    groups.push({
      heading: "ENTRY POINTS",
      items: entryPoints.map((e) => ({
        id: `entry:${e.folderId}:${e.ep.id}`,
        title: `${e.ep.verb} ${e.ep.target}`,
        subtitle: data.multi ? e.folderLabel : undefined,
        icon: Workflow,
        run: () => actions.openEntryPoint(e.folderId, e.ep),
      })),
    });
  }

  const files = rank(
    trimmed,
    data.files,
    (f) => ({ title: fileName(f.path), others: [f.path] }),
    LIMIT,
  );
  if (files.length > 0) {
    groups.push({
      heading: "FILES",
      items: files.map((f) => ({
        id: `file:${f.folderId}:${f.path}`,
        title: fileName(f.path),
        subtitle: where(f.folderLabel, directoryOf(f.path)),
        icon: fileIconComponent(f.path),
        run: () => actions.openFile(f.folderId, f.path),
      })),
    });
  }

  const symbols = rank(
    trimmed,
    data.symbols,
    (s) => ({ title: symbolDisplay(s.match).name, others: [s.match.label] }),
    LIMIT,
  );
  if (symbols.length > 0) {
    groups.push({
      heading: "SYMBOLS",
      items: symbols.map((s) => {
        const { name, detail } = symbolDisplay(s.match);
        return {
          id: `symbol:${s.folderId}:${s.match.nodeId}`,
          title: name,
          subtitle: where(s.folderLabel, detail),
          icon: s.match.kind === "class" ? CodeXml : SquareFunction,
          run: () => actions.openSymbol(s.folderId, s.match),
        };
      }),
    });
  }

  return groups;
}

let fileCache: { data: unknown; key: string; items: PaletteFile[] } | null = null;

/**
 * Code Graph's contribution to the command palette: entry points, files and
 * symbols of the folders in view.
 *
 * `ensureStarted` wires the module up from wherever it has not run yet this
 * session. Everything is read from what the store already holds or, for symbols,
 * from the folders' index files — nothing opens a project to answer, so typing in
 * the palette never starts an analysis.
 */
export async function searchPalette(query: string, signal: AbortSignal): Promise<PaletteSearchGroup[]> {
  const trimmed = query.trim();
  if (!trimmed) return [];

  try {
    const [actions, { useCodeGraphStore }, bridge, { scopeFolders }, { folderLabels }] = await Promise.all([
      import("./codegraphActions"),
      import("./codegraphStore"),
      import("@/bridge/workspaceCode"),
      import("./workspace/scope"),
      import("./workspace/identity"),
    ]);
    actions.ensureStarted();
    if (signal.aborted) return [];

    const state = useCodeGraphStore.getState();
    if (!state.workspaceId || state.folders.length === 0) return [];
    const inView = scopeFolders(state.scope, state.folders);
    const labels = folderLabels(state.folders.map((f) => ({ id: f.id, name: f.name, path: f.path })));

    const entryPoints: PaletteEntry[] = inView.flatMap((f) =>
      (state.data[f.id]?.entries?.entries ?? []).map((ep) => ({ folderId: f.id, folderLabel: labels[f.id], ep })),
    );

    const key = inView.map((f) => f.id).join(",");
    if (!fileCache || fileCache.data !== state.data || fileCache.key !== key) {
      fileCache = {
        data: state.data,
        key,
        items: inView.flatMap((f) =>
          (state.data[f.id]?.files ?? []).filter((e) => !e.isDir).map((e) => ({ folderId: f.id, folderLabel: labels[f.id], path: e.path })),
        ),
      };
    }

    let symbols: PaletteSymbol[] = [];
    if (trimmed.length >= MIN_SYMBOL_QUERY) {
      try {
        const found = await bridge.searchWorkspaceSymbols(state.workspaceId, inView.map((f) => f.id), trimmed);
        symbols = found.flatMap((f) => f.matches.map((match) => ({ folderId: f.folderId, folderLabel: labels[f.folderId] ?? "", match })));
      } catch {
        symbols = [];
      }
      if (signal.aborted) return [];
    }

    const owner = (folderId: string) => ({ workspaceId: state.workspaceId, folderId });
    return buildGroups(
      query,
      { entryPoints, files: fileCache.items, symbols, multi: inView.length > 1 },
      {
        openEntryPoint: (folderId, ep) => actions.viewFlow(owner(folderId), ep, { preview: false }),
        openFile: (folderId, path) => void actions.openFile({ ...owner(folderId), path }, null, { preview: false }),
        openSymbol: (folderId, match) => actions.viewSymbol(owner(folderId), match, { preview: false }),
      },
    );
  } catch {
    return [];
  }
}
