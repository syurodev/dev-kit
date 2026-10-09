import type { SearchFileResult, SearchMatch, SearchResult } from "@/bridge/workspaceCode";
import type { Scope } from "./scope";

/**
 * Find in workspace, without the UI.
 *
 * A search is a *session*: its scope, its options, and the one result it is
 * showing. The session's scope is its own — it starts as a copy of the
 * discovery scope (or of the folder a context menu was opened on) and then stays
 * what the form says, whatever the sidebar's scope does afterwards.
 */

export type SearchOptions = {
  query: string;
  regex: boolean;
  caseSensitive: boolean;
  wholeWord: boolean;
  /** Comma-separated path patterns; blank means everything. */
  include: string;
  exclude: string;
};

export const DEFAULT_SEARCH_OPTIONS: SearchOptions = {
  query: "",
  regex: false,
  caseSensitive: false,
  wholeWord: false,
  include: "",
  exclude: "",
};

/** Waiting for a pause in typing before asking Go. */
export const SEARCH_DEBOUNCE_MS = 250;

export type SearchStatus = "idle" | "searching" | "done" | "invalid" | "failed";

export type SearchSession = {
  id: string;
  workspaceId: string;
  scope: Scope;
  options: SearchOptions;
  status: SearchStatus;
  /** Why it is invalid or failed. */
  error: string;
  result: SearchResult | null;
  /** The options the result was computed for — a result is stale once these differ. */
  resultFor: string;
  /** Group keys folded shut. */
  collapsed: string[];
};

/** Patterns from the include/exclude fields: split on commas, trimmed, blanks dropped. */
export function splitPatterns(text: string): string[] {
  return text
    .split(",")
    .map((p) => p.trim())
    .filter(Boolean);
}

/** A search can run: there is something to look for. */
export function searchable(o: SearchOptions): boolean {
  return o.query.length > 0;
}

/**
 * The identity of a request, for dropping a result that answers a question
 * since changed: workspace, scope folders and every option that changes hits.
 */
export function requestSignature(workspaceId: string, folderIds: string[], o: SearchOptions): string {
  return JSON.stringify([workspaceId, folderIds, o.query, o.regex, o.caseSensitive, o.wholeWord, splitPatterns(o.include), splitPatterns(o.exclude)]);
}

export type FolderRow = {
  kind: "folder";
  key: string;
  folderId: string;
  files: number;
  matches: number;
  collapsed: boolean;
  state: "ok" | "unavailable" | "error" | "duplicate";
  error: string;
};
export type FileRow = { kind: "file"; key: string; folderId: string; path: string; matches: number; truncated: boolean; collapsed: boolean };
export type MatchRow = { kind: "match"; key: string; folderId: string; path: string; match: SearchMatch };
export type ResultRow = FolderRow | FileRow | MatchRow;

export const folderGroupKey = (folderId: string) => `f:${folderId}`;
export const fileGroupKey = (folderId: string, path: string) => `p:${folderId}\u0000${path}`;

/**
 * The flat rows a virtualized list draws: folder → file → matches, in the order
 * Go returned them (folder order, then path, then position). Folders that were
 * searched but have no hits are listed only when something is wrong with them
 * (unavailable, failed) or when asked for — "searched, found nothing" is the
 * summary line's job, not twenty empty headings.
 */
export function buildRows(result: SearchResult, collapsed: readonly string[], opts: { showEmptyFolders?: boolean } = {}): ResultRow[] {
  const folded = new Set(collapsed);
  const byFolder = new Map<string, SearchFileResult[]>();
  for (const f of result.files) byFolder.set(f.folderId, [...(byFolder.get(f.folderId) ?? []), f]);

  const rows: ResultRow[] = [];
  for (const folder of result.folders) {
    const files = byFolder.get(folder.folderId) ?? [];
    const matches = files.reduce((n, f) => n + f.matches.length, 0);
    const troubled = folder.state !== "ok";
    if (files.length === 0 && !troubled && !opts.showEmptyFolders) continue;
    const key = folderGroupKey(folder.folderId);
    const closed = folded.has(key);
    rows.push({ kind: "folder", key, folderId: folder.folderId, files: files.length, matches, collapsed: closed, state: folder.state, error: folder.error });
    if (closed) continue;
    for (const file of files) {
      const fkey = fileGroupKey(folder.folderId, file.path);
      const fileClosed = folded.has(fkey);
      rows.push({ kind: "file", key: fkey, folderId: folder.folderId, path: file.path, matches: file.matches.length, truncated: file.truncated, collapsed: fileClosed });
      if (fileClosed) continue;
      file.matches.forEach((match, i) => rows.push({ kind: "match", key: `${fkey}\u0000${i}`, folderId: folder.folderId, path: file.path, match }));
    }
  }
  return rows;
}

/**
 * The text before a hit, cut so the hit starts near the left edge of a narrow
 * row. A row truncates on the right, so without this a hit late in a line is
 * the part that is not visible.
 */
export function leadingContext(before: string, keep = 14): string {
  const text = before.trimStart();
  if (text.length <= keep) return text;
  // Never split a surrogate pair.
  let from = text.length - keep;
  const c = text.charCodeAt(from);
  if (c >= 0xdc00 && c <= 0xdfff) from++;
  return `…${text.slice(from)}`;
}

/** A preview split around the hit, for highlighting. Offsets are UTF-16 units, as the preview is. */
export function previewParts(match: SearchMatch): { before: string; hit: string; after: string } {
  const start = Math.max(0, Math.min(match.preview.length, match.column - 1 - match.previewStart));
  const end = Math.max(start, Math.min(match.preview.length, start + match.length));
  return { before: match.preview.slice(0, start), hit: match.preview.slice(start, end), after: match.preview.slice(end) };
}

/** The one line under the form saying what the result is and is not. */
export function resultSummary(result: SearchResult): string {
  const files = result.files.length;
  const hits = `${result.matches} match${result.matches === 1 ? "" : "es"} in ${files} file${files === 1 ? "" : "s"}`;
  const searched = `${result.filesSearched} searched`;
  const skipped = result.filesSkipped > 0 ? `, ${result.filesSkipped} skipped (binary, large or links)` : "";
  const cut =
    result.truncated === "matches"
      ? " — showing the first matches only; narrow the search to see the rest"
      : result.truncated === "files"
        ? " — too many files; some were not searched"
        : "";
  // A file stops at 500 hits so one lockfile cannot fill the list; say so, because its count is not the file's total.
  const capped = result.files.filter((f) => f.truncated).length;
  const cappedNote = capped > 0 ? ` — ${capped} file${capped === 1 ? "" : "s"} cut at ${PER_FILE_CAP} matches` : "";
  return `${hits} · ${searched}${skipped}${cut}${cappedNote}`;
}

/** Mirrors maxMatchesPerFile in internal/wssearch. */
export const PER_FILE_CAP = 500;

/** Folders that took no part in the search, with why, for a notice. */
export function troubledFolders(result: SearchResult): Array<{ folderId: string; state: string; error: string }> {
  return result.folders.filter((f) => f.state !== "ok" && f.state !== "duplicate").map((f) => ({ folderId: f.folderId, state: f.state, error: f.error }));
}
