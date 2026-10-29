import { agentHubGetStatus, agentHubPrepareSession, isSessionClosedError } from "../../../bridge/agenthub";
import { useAgentSessionsStore } from "../../../stores/agentSessions";

type Bridge = {
  prepare: typeof agentHubPrepareSession;
  getStatus: typeof agentHubGetStatus;
};

/**
 * The call behind the session config query: wake the session so its controls can
 * be read. Resolves true once prepared.
 *
 * A closed session is refused, and the window asked because its copy of the row
 * is stale (a parent agent closed the child through MCP, the window never heard).
 * That is not a failure to show: the row is read again so it says closed, which
 * also turns the query off, and the call resolves false.
 */
export async function prepareSessionConfig(
  sessionId: string,
  bridge: Bridge = { prepare: agentHubPrepareSession, getStatus: agentHubGetStatus },
): Promise<boolean> {
  try {
    await bridge.prepare(sessionId);
    return true;
  } catch (error) {
    if (!isSessionClosedError(error)) throw error;
  }
  try {
    useAgentSessionsStore.getState().upsertSession(await bridge.getStatus(sessionId));
  } catch {
    // Nothing to correct it with: the answer stays cached for the query's stale time.
  }
  return false;
}
