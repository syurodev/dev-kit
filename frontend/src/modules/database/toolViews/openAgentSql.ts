import { toast } from "sonner";
import type { ConnectionProfile } from "@/bridge/database";
import { useDatabaseStore } from "../databaseStore";
import { openSqlConsole } from "../documents";

/** What a console opened from an agent's result is called — it holds only SQL. */
export const AGENT_QUERY_TITLE = "Agent query";

type ProfileState = { profiles: Pick<ConnectionProfile, "id" | "name">[]; selectedProfileId: string };

/**
 * What to tell the person about the connection when an agent's statement
 * opens in a console. The console runs on the connection selected here,
 * which need not be the one the agent used: db.explain names its profile
 * (compared), db.query names none (said so). Null when they match.
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
 * Open in Database, from a db.query or db.explain card: the statement in a
 * console beside the transcript, never run — the person decides whether,
 * and where. Returns what it told them about the connection.
 */
export function openAgentSql(
  sql: string,
  agentProfileId: string | undefined,
  open: typeof openSqlConsole = openSqlConsole,
): string | null {
  open(sql, AGENT_QUERY_TITLE, { beside: true });
  const notice = agentSqlNotice(agentProfileId, useDatabaseStore.getState());
  if (notice) toast.info("Opened, not run", { description: notice });
  return notice;
}
