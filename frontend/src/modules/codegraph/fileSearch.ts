import type { CodeGraphFileEntry } from "@/bridge/codegraph";

export type FileSearchResult = {
  entries: CodeGraphFileEntry[];
  /**
   * Folders that must render open for the matches to be seen. Null when no
   * query is active, so the tree falls back to what the user expanded.
   */
  forceExpanded: Set<string> | null;
};

function ancestorsOf(path: string): string[] {
  const out: string[] = [];
  let idx = path.lastIndexOf("/");
  while (idx !== -1) {
    path = path.slice(0, idx);
    out.push(path);
    idx = path.lastIndexOf("/");
  }
  return out;
}

/**
 * Filters the file tree by a search needle without hiding what matched.
 *
 * The tree only shows an entry whose every ancestor folder is expanded, so
 * filtering the flat list alone left a nested match with no visible parent —
 * searching "UserService" found src/main/java/.../UserService.java and showed
 * nothing. A match therefore brings its ancestor folders along, and they are
 * reported as expanded for as long as the query is active.
 *
 * Input order is preserved (the backend lists folders before their contents).
 */
export function filterFileEntries(
  entries: CodeGraphFileEntry[],
  query: string,
  /**
   * The Files tab's toggles (Java only, Changed, In flow): a file must pass it
   * to be shown. Folders are never tested — they come along with what they hold.
   */
  keepFile?: (path: string) => boolean,
): FileSearchResult {
  const needle = query.trim().toLowerCase();
  if (!needle && !keepFile) return { entries, forceExpanded: null };
  const keep = new Set<string>();
  const forceExpanded = new Set<string>();
  for (const e of entries) {
    if (keepFile && (e.isDir || !keepFile(e.path))) continue;
    if (needle && !e.path.toLowerCase().includes(needle)) continue;
    keep.add(e.path);
    for (const a of ancestorsOf(e.path)) {
      keep.add(a);
      forceExpanded.add(a);
    }
  }
  // Only a search opens folders: a toggle alone leaves the tree as it was folded.
  return { entries: entries.filter((e) => keep.has(e.path)), forceExpanded: needle ? forceExpanded : null };
}

function parentOf(path: string): string | null {
  const idx = path.lastIndexOf("/");
  return idx === -1 ? null : path.slice(0, idx);
}

/**
 * Every entry whose full ancestor chain is in `expanded` — a depth-0 entry is
 * always visible. The test that a search match can actually be seen, not
 * merely that it survived the filter.
 */
export function visibleEntries(entries: CodeGraphFileEntry[], expanded: Set<string>): CodeGraphFileEntry[] {
  return entries.filter((e) => {
    let parent = parentOf(e.path);
    while (parent !== null) {
      if (!expanded.has(parent)) return false;
      parent = parentOf(parent);
    }
    return true;
  });
}
