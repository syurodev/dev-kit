import type { AgentSession } from "@/bridge/agenthub";
import { openAgentSession } from "../documents";
import { useSubagentPanel } from "../subagents";
import type { Subagent } from "../types";

/**
 * A subagent's work, beside its session: the session's tab (the same
 * `documents.ts` helper every other way in uses, so a tab already open is
 * focused rather than duplicated) and the module's right-hand pane on that
 * subagent. A subagent has no session of DevKit's to open, so it opens the one
 * that owns it.
 */
export function showSubagent(session: Pick<AgentSession, "id" | "title">, subagent: Pick<Subagent, "id">): void {
  useSubagentPanel.getState().show(session.id, subagent.id);
  openAgentSession(session);
}
