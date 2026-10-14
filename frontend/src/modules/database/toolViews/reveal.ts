import type { EntityRef } from "@/workbench/intents";

/** What a `database.reveal` ref opens. */
export type RevealTarget =
  | { open: "table"; schema: string; table: string }
  | { open: "console"; sql: string; title: string };

/**
 * Routes a `database.reveal` ref, pure so the routing is testable without a
 * workbench: `{kind:"table", id:"schema.table"}` opens that table's tab,
 * `{kind:"console", id: sql}` a console holding the SQL. The table id is split
 * at its first dot — a schema name with a dot in it is rarer than a table
 * name with one — and a table with no schema is null: the table view needs
 * both, and guessing a default schema would open the wrong table on MySQL.
 */
export function revealTarget(ref: Pick<EntityRef, "kind" | "id" | "label">): RevealTarget | null {
  if (ref.kind === "table") {
    const dot = ref.id.indexOf(".");
    if (dot <= 0 || dot === ref.id.length - 1) return null;
    return { open: "table", schema: ref.id.slice(0, dot), table: ref.id.slice(dot + 1) };
  }
  if (ref.kind === "console") {
    const sql = ref.id.trim();
    if (!sql) return null;
    return { open: "console", sql, title: ref.label?.trim() || "Console" };
  }
  return null;
}
