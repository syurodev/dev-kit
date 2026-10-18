import type { AgentSession } from "@/bridge/agenthub";
import { useAgentPinsStore } from "@/stores/agentPins";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { setAgentHubTabsStatus } from "@/workbench/agentPromptQueue";
import { dismissalAt } from "./dismissed";
import { attentionBySession, failureDismissable } from "./model";

/**
 * Says "I do not need this failure any more": the session leaves Needs you,
 * stops marking its parent, and its tab and "ready to review" mark settle. It
 * is not closed or touched in Go, and it returns to Needs you only by failing
 * again (dismissed.ts). Reads the stores at the moment of the call, so a click
 * on a stale row cannot dismiss a session that has since started waiting on a
 * permission, or one that already recovered.
 */
export function dismissSessionFailure(session: Pick<AgentSession, "id">): boolean {
  const sessions = useAgentSessionsStore.getState();
  const current = sessions.sessionsById[session.id];
  if (!current) return false;
  const pins = useAgentPinsStore.getState();
  const attention = attentionBySession(sessions.pendingQueue, sessions.elicitationQueue)[current.id];
  if (!failureDismissable(current, attention, pins.dismissedFailures)) return false;
  pins.dismissFailure(current.id, dismissalAt(current));
  sessions.clearCompletedUnread(current.id);
  setAgentHubTabsStatus(current.id, "idle");
  return true;
}
