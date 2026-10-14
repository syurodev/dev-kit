import type { ParsedSchema } from "./parse";

/** Rows a card lists folded — a schema can hold hundreds of tables. */
export const LIST_CAP = 40;
/** Rows a card draws opened: no virtualization here, so a ceiling instead. */
export const LIST_MAX = 500;

export type ListWindow = {
  /** How many to draw. */
  shown: number;
  /** The toggle's label, or null when the list is short enough to need none. */
  toggle: string | null;
  /** Left out even when opened (past LIST_MAX). */
  notShown: number;
};

/** How much of a list of `total` a card draws, folded or opened. */
export function listWindow(total: number, all: boolean): ListWindow {
  if (total <= LIST_CAP) return { shown: total, toggle: null, notShown: 0 };
  if (all) {
    const shown = Math.min(total, LIST_MAX);
    return { shown, toggle: "Show fewer", notShown: total - shown };
  }
  return { shown: LIST_CAP, toggle: total > LIST_MAX ? `Show first ${LIST_MAX}` : `Show all ${total}`, notShown: 0 };
}

/**
 * The schemas to draw with at most `limit` tables between them, spent in
 * order. Once the budget is gone nothing more is drawn — not even an empty
 * schema, which would otherwise sit below a list that stopped short.
 */
export function visibleSchemas(schemas: ParsedSchema["schemas"], limit: number): ParsedSchema["schemas"] {
  const out: ParsedSchema["schemas"] = [];
  let budget = limit;
  for (const schema of schemas) {
    if (budget <= 0) break;
    const tables = schema.tables.slice(0, budget);
    budget -= tables.length;
    out.push({ name: schema.name, tables });
  }
  return out;
}

/** A table's key for React and the expanded set: NUL cannot occur in either name. */
export function tableKey(schema: string, table: string): string {
  return `${schema}\u0000${table}`;
}
