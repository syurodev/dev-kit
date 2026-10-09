/**
 * Whether `path` is `root` or inside it, on "/"-separated absolute paths — a
 * prefix of a folder name (kong-plugin-v2 against kong-plugin) is not inside.
 * Mirrors Go's workspace.Within, which has the same rule.
 */
export function Within(path: string, root: string): boolean {
  if (!path || !root) return false;
  const p = path.replace(/\/+$/, "");
  const r = root.replace(/\/+$/, "");
  return p === r || p.startsWith(`${r}/`);
}

const slashes = (p: string) => p.split("\\").join("/");

/**
 * The paths, relative to `folder`, of the other folders that sit inside it.
 * A file under one of them has two possible owners; the workspace gives it to
 * the deepest (the nested folder), so the outer folder leaves it out of its
 * own listing and search results (docs/design/codegraph-workspace.md § 3.1).
 */
export function nestedRoots(folder: { id: string; path: string }, others: readonly { id: string; path: string }[]): string[] {
  const outer = slashes(folder.path).replace(/\/+$/, "");
  const out: string[] = [];
  for (const o of others) {
    if (o.id === folder.id) continue;
    const inner = slashes(o.path).replace(/\/+$/, "");
    if (inner.length > outer.length && inner.startsWith(`${outer}/`)) out.push(inner.slice(outer.length + 1));
  }
  return out;
}

/**
 * Drops the entries that belong to a nested folder (`prefixes` from
 * nestedRoots), and the directories that held nothing else — a `libs` folder
 * that only contained the nested one is not drawn empty.
 */
export function withoutNested<T extends { path: string }>(entries: readonly T[], prefixes: readonly string[]): T[] {
  if (prefixes.length === 0) return entries as T[];
  const inNested = (path: string) => prefixes.some((p) => path === p || path.startsWith(`${p}/`));
  const kept = entries.filter((e) => !inNested(e.path));
  // Ancestors of a removed entry that no remaining entry lives under.
  const suspect = new Set<string>();
  for (const e of entries) {
    if (!inNested(e.path)) continue;
    for (let at = e.path.lastIndexOf("/"); at > 0; at = e.path.lastIndexOf("/", at - 1)) suspect.add(e.path.slice(0, at));
  }
  if (suspect.size === 0) return kept;
  const occupied = new Set<string>();
  for (const e of kept) for (let at = e.path.lastIndexOf("/"); at > 0; at = e.path.lastIndexOf("/", at - 1)) occupied.add(e.path.slice(0, at));
  return kept.filter((e) => !suspect.has(e.path) || occupied.has(e.path));
}
