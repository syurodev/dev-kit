import type { EntityRef, Placement } from "@/workbench/intents";

/** What a `database.reveal` ref opens. `profileId`: the connection the table was read on. */
export type RevealTarget =
  | { open: "table"; schema: string; table: string; profileId?: string }
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
export function revealTarget(ref: Pick<EntityRef, "kind" | "id" | "label" | "profileId">): RevealTarget | null {
  if (ref.kind === "table") {
    const parts = splitTableId(ref.id);
    if (!parts) return null;
    return ref.profileId ? { open: "table", ...parts, profileId: ref.profileId } : { open: "table", ...parts };
  }
  if (ref.kind === "console") {
    const sql = ref.id.trim();
    if (!sql) return null;
    return { open: "console", sql, title: ref.label?.trim() || "Console" };
  }
  return null;
}

/** The store's connections and its selection; `liveProfileSelection` wires the live ones. */
export type ProfileSelection = {
  profileIds: () => readonly string[];
  selectedProfileId: () => string;
  /** Loads the profile list again (the store's fetchProfiles). */
  refreshProfiles: () => Promise<void>;
  /** The store's own selection — the rail's click — so whatever a switch does, it does here too. */
  selectProfile: (id: string) => void;
  /** What switching to `id` would lose here, or null when nothing. */
  switchCost: (id: string) => SwitchCost | null;
  /** Asks the person; true to go ahead. */
  confirm: (message: string) => Promise<boolean>;
};

/**
 * What the rail's switch throws away: it gives the old connection's session
 * back (ending an open transaction) and stops every console run in flight.
 */
export type SwitchCost = { from: string; to: string; transaction: boolean; runningQueries: number };

/** The question asked before a switch that loses something, naming what. */
export function switchLossMessage(cost: SwitchCost): string {
  const parts: string[] = [];
  if (cost.transaction) parts.push(`rolls back the open transaction on ${cost.from}`);
  if (cost.runningQueries > 0) {
    const n = cost.runningQueries;
    parts.push(`stops ${n} running ${n === 1 ? "query" : "queries"}${cost.transaction ? "" : ` on ${cost.from}`}`);
  }
  return `Switching to ${cost.to} ${parts.join(" and ")}. Switch?`;
}

/**
 * Selects the connection an agent's call ran on, as a click in the rail
 * would. A connection this machine does not have (asked for once more, in
 * case the list was not loaded) is "unavailable", as is a selection that did
 * not take — the caller then opens nothing rather than open on whichever
 * connection happened to be selected, which may be prod.
 *
 * A switch that loses nothing happens without a word. One that would end an
 * open transaction or stop a running query asks first (`switchLossMessage`);
 * "cancelled" when the person says no, and the caller opens nothing.
 */
export async function selectProfileById(
  profileId: string,
  deps: ProfileSelection,
): Promise<"selected" | "unavailable" | "cancelled"> {
  if (!deps.profileIds().includes(profileId)) {
    await deps.refreshProfiles();
    if (!deps.profileIds().includes(profileId)) return "unavailable";
  }
  if (deps.selectedProfileId() !== profileId) {
    const cost = deps.switchCost(profileId);
    if (cost && !(await deps.confirm(switchLossMessage(cost)))) return "cancelled";
    deps.selectProfile(profileId);
  }
  return deps.selectedProfileId() === profileId ? "selected" : "unavailable";
}

/** What `reveal` needs from the store and the workbench; the manifest wires the live ones. */
export type RevealDeps = ProfileSelection & {
  /** Opens a table on the selected connection. */
  openTable: (schema: string, table: string, beside: boolean) => void;
  openConsole: (sql: string, title: string, beside: boolean) => Promise<void>;
};

/**
 * Runs a `database.reveal`. A table that names its connection opens on that
 * connection: it is selected first (`selectProfileById`) and only then
 * opened; one that cannot be selected is "unavailable", and a switch the
 * person declined opens nothing and is "ok" (there is nothing to report). Without one, the
 * table opens on the selected connection, as it always has.
 */
export async function reveal(ref: EntityRef, placement: Placement, deps: RevealDeps): Promise<"ok" | "unavailable"> {
  const target = revealTarget(ref);
  if (!target) return "unavailable";
  const beside = placement === "beside";
  if (target.open === "console") {
    await deps.openConsole(target.sql, target.title, beside);
    return "ok";
  }
  if (target.profileId) {
    const selection = await selectProfileById(target.profileId, deps);
    // Cancelled is the person's answer, not a failure: nothing to report.
    if (selection === "cancelled") return "ok";
    if (selection === "unavailable") return "unavailable";
  }
  deps.openTable(target.schema, target.table, beside);
  return "ok";
}
