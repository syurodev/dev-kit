import type { SchemaNamespace } from "../../bridge/database";

/**
 * What the sidebar's one search box acts on.
 *
 * It filters two different depths of the same tree — connections near the
 * root, tables three levels down — so the matching lives here rather than
 * inline: a needle that silently stops reaching one of them is the kind of
 * bug the box looks fine while having.
 */

/** A connection matches on anything a person would type looking for it. */
export function matchesProfile(
  profile: { name: string; database: string; host: string },
  needle: string,
): boolean {
  if (!needle) return true;
  const n = needle.toLowerCase();
  return (
    profile.name.toLowerCase().includes(n) ||
    profile.database.toLowerCase().includes(n) ||
    profile.host.toLowerCase().includes(n)
  );
}

/**
 * The namespace with non-matching tables removed, and any schema left with no
 * table dropped with them — an empty schema row under a search reads as "this
 * schema has nothing", which is not what happened.
 *
 * An empty needle returns the namespace unchanged (same object), so the common
 * case allocates nothing.
 */
export function filterNamespace(
  namespace: SchemaNamespace,
  needle: string,
): SchemaNamespace {
  if (!needle) return namespace;
  const n = needle.toLowerCase();
  const out: SchemaNamespace = {};
  for (const [schema, tables] of Object.entries(namespace)) {
    const kept = Object.entries(tables || {}).filter(([table]) =>
      table.toLowerCase().includes(n),
    );
    if (kept.length > 0) out[schema] = Object.fromEntries(kept);
  }
  return out;
}

/** Every table in the namespace, for the count on a connection row. */
export function countTables(namespace: SchemaNamespace): number {
  return Object.values(namespace).reduce(
    (total, tables) => total + Object.keys(tables || {}).length,
    0,
  );
}
