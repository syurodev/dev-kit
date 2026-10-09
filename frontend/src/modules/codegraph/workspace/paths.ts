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
