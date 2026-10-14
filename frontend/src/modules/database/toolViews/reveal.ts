import type { EntityRef } from "@/workbench/intents";

/** What a `database.reveal` ref opens. */
export type RevealTarget =
  | { open: "table"; schema: string; table: string }
  | { open: "console"; sql: string; title: string };

/**
 * `schema.table` → its two halves. A schema written in SQL double quotes
 * keeps its dots (`"sales.eu".orders`, `""` inside for a quote); otherwise
 * the id splits at its first dot — a schema name with a dot is rarer than a
 * table name with one. Null when either half is missing: the table view
 * needs both, and guessing a default schema would open the wrong table on
 * MySQL.
 */
function splitTableId(id: string): { schema: string; table: string } | null {
  let schema: string;
  let rest: string;
  if (id.startsWith('"')) {
    let i = 1;
    schema = "";
    for (;;) {
      if (i >= id.length) return null;
      if (id[i] === '"') {
        if (id[i + 1] === '"') {
          schema += '"';
          i += 2;
          continue;
        }
        break;
      }
      schema += id[i];
      i += 1;
    }
    if (id[i + 1] !== ".") return null;
    rest = id.slice(i + 2);
  } else {
    const dot = id.indexOf(".");
    if (dot < 0) return null;
    schema = id.slice(0, dot);
    rest = id.slice(dot + 1);
  }
  if (!schema || !rest) return null;
  return { schema, table: rest };
}

/**
 * Routes a `database.reveal` ref, pure so the routing is testable without a
 * workbench: `{kind:"table", id:"schema.table"}` opens that table's tab,
 * `{kind:"console", id: sql}` a console holding the SQL.
 */
export function revealTarget(ref: Pick<EntityRef, "kind" | "id" | "label">): RevealTarget | null {
  if (ref.kind === "table") {
    const parts = splitTableId(ref.id);
    return parts ? { open: "table", ...parts } : null;
  }
  if (ref.kind === "console") {
    const sql = ref.id.trim();
    if (!sql) return null;
    return { open: "console", sql, title: ref.label?.trim() || "Console" };
  }
  return null;
}
