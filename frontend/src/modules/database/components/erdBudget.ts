export const ERD_NODE_LIMIT = 100;
export const ERD_EDGE_LIMIT = 400;
export const ERD_COLUMN_LIMIT = 80;
/** Bounded breadth-first subgraph. Stable ordering keeps a focused table first. */
export function erdTableWindow(
  tables: Record<string, string[]>,
  neighbours: Record<string, Set<string>>,
  focus: string,
  depth: number,
) {
  const names = Object.keys(tables).sort();
  if (!tables[focus])
    return {
      names: names.slice(0, ERD_NODE_LIMIT),
      hops: {} as Record<string, number>,
      limited: names.length > ERD_NODE_LIMIT,
    };
  const hops: Record<string, number> = { [focus]: 0 };
  const selected = [focus];
  let frontier = [focus],
    limited = false;
  for (let hop = 1; hop <= depth; hop++) {
    const next: string[] = [];
    for (const name of frontier) {
      for (const other of [...(neighbours[name] ?? [])].sort()) {
        if (!tables[other] || hops[other] !== undefined) continue;
        if (selected.length >= ERD_NODE_LIMIT) {
          limited = true;
          break;
        }
        hops[other] = hop;
        selected.push(other);
        next.push(other);
      }
    }
    if (limited) break;
    frontier = next;
  }
  return { names: selected, hops, limited };
}

/** Preserve relation handles first; even one very wide table has bounded DOM. */
export function erdColumnWindow(
  columns: readonly string[],
  keys: readonly string[],
  expanded: boolean,
): string[] {
  const keySet = new Set(keys);
  const important = columns
    .filter((name) => keySet.has(name))
    .slice(0, ERD_COLUMN_LIMIT);
  return expanded
    ? [
        ...important,
        ...columns
          .filter((name) => !keySet.has(name))
          .slice(0, ERD_COLUMN_LIMIT - important.length),
      ]
    : important;
}

/** A column named id alone is not evidence of a primary key. */
export function hasSingleIdPrimaryKey(
  indexes: readonly {
    table: string;
    primary: boolean;
    columns: readonly string[];
  }[],
  table: string,
): boolean {
  return indexes.some(
    (index) =>
      index.table === table &&
      index.primary &&
      index.columns.length === 1 &&
      index.columns[0] === "id",
  );
}
