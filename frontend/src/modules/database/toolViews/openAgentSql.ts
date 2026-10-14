import { toast } from "sonner";
import type { ConnectionProfile, QueryResult } from "@/bridge/database";
import { useDatabaseStore } from "../databaseStore";
import { openSqlConsole } from "../documents";
import { liveProfileSelection } from "./liveProfiles";
import { selectProfileById, type ProfileSelection } from "./reveal";

/** What a console opened from an agent's result is called. */
export const AGENT_QUERY_TITLE = "Agent query";

type ProfileState = { profiles: Pick<ConnectionProfile, "id" | "name">[]; selectedProfileId: string };

/**
 * What to tell the person about the connection when an agent's statement
 * opens in a console. A call that names its connection (db.query's
 * `profile_id`, db.explain's `profileId`) is switched to before it opens, so
 * this speaks only for one that does not — an older transcript's db.query —
 * or one that somehow differs. Null when they match.
 */
export function agentSqlNotice(agentProfileId: string | undefined, state: ProfileState): string | null {
  const name = (id: string) => state.profiles.find((p) => p.id === id)?.name;
  const here = (state.selectedProfileId && name(state.selectedProfileId)) || "no connection";
  if (agentProfileId === undefined) {
    return `This result does not say which connection the agent used; this console is on ${here}.`;
  }
  if (agentProfileId === state.selectedProfileId) return null;
  return `The agent used ${name(agentProfileId) ?? "another connection"}; this console is on ${here}.`;
}

/**
 * Where a db.query ran, for its card's header: `on <name>` when this device
 * has the connection, `on another connection` when the loaded list does not,
 * and nothing when the result names none (an older transcript) or the list
 * has not been loaded — a card fetches nothing to find out.
 */
export function connectionPhrase(
  profileId: string | undefined,
  profiles: readonly Pick<ConnectionProfile, "id" | "name">[],
): string | null {
  if (!profileId || profiles.length === 0) return null;
  const name = profiles.find((p) => p.id === profileId)?.name;
  return name ? `on ${name}` : "on another connection";
}

export type OpenAgentSqlOptions = {
  /** The rows the card holds (db.query), shown in the console without running. */
  result?: QueryResult;
  open?: typeof openSqlConsole;
  profiles?: ProfileSelection;
};

/**
 * Open in Database, from a db.query or db.explain card: the statement in a
 * console beside the transcript, never run — the person decides whether.
 *
 * A call that names its connection is opened on it: selected first, the way
 * the rail selects it, and one not on this device (or a selection that did
 * not take) opens nothing and says why. A switch that would end an open
 * transaction or stop a running query asks first; Cancel opens nothing. One that names none opens on the
 * selected connection with a notice. A db.query's rows come along and show
 * as the console's result (`openSqlConsole`'s `result`), labelled as the
 * agent's, until the person runs it.
 */
export async function openAgentSql(
  sql: string,
  agentProfileId: string | undefined,
  options: OpenAgentSqlOptions = {},
): Promise<{ opened: boolean; notice: string | null }> {
  const open = options.open ?? openSqlConsole;
  if (agentProfileId !== undefined) {
    const selection = await selectProfileById(agentProfileId, options.profiles ?? liveProfileSelection);
    if (selection === "cancelled") return { opened: false, notice: null };
    if (selection === "unavailable") {
      toast.error("Couldn't open in Database: its connection isn't on this device");
      return { opened: false, notice: null };
    }
  }
  await open(sql, AGENT_QUERY_TITLE, options.result ? { beside: true, result: options.result } : { beside: true });
  // Selected above when named, so only a result that names none has
  // anything to say about the connection.
  const notice = agentProfileId === undefined ? agentSqlNotice(undefined, useDatabaseStore.getState()) : null;
  if (notice) toast.info("Opened, not run", { description: notice });
  return { opened: true, notice };
}
