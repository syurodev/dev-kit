import { agentHubCancel, agentHubGetStatus } from "@/bridge/agenthub";
import { useAgentSessionsStore } from "@/stores/agentSessions";

/**
 * Ends a session's turn: Go stops its whole runtime (the turn, its subagents
 * and its background tasks), what waited on the person for it is dropped, and
 * its row is read again. The one implementation behind the sidebar's "Cancel
 * turn", the composer's stop and the subagent pane's "Stop turn"; each says
 * how a failure is shown.
 */
export async function cancelSessionTurn(sessionId: string, reportError: (error: unknown) => void): Promise<void> {
  try {
    await agentHubCancel(sessionId);
    const store = useAgentSessionsStore.getState();
    store.dequeuePendingForSession(sessionId);
    store.dequeueElicitationForSession(sessionId);
    store.upsertSession(await agentHubGetStatus(sessionId));
  } catch (e) {
    reportError(e);
  }
}
