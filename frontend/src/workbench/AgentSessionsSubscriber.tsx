import { useEffect } from "react";
import { agentHubListAgents, onAgentHubPermission } from "../bridge/agenthub";
import { useAgentSessionsStore } from "../stores/agentSessions";

/**
 * Keeps the shared Agent Hub session store in sync regardless of which
 * module is mounted. Rendered once in Workbench.tsx, as a sibling of the
 * sidebar and content card — never remounted on module switch, unlike
 * AgentHubView which only exists while that module is active.
 */
export function AgentSessionsSubscriber() {
  const setSessions = useAgentSessionsStore((state) => state.setSessions);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);

  useEffect(() => {
    let mounted = true;
    agentHubListAgents()
      .then((sessions) => {
        if (mounted) {
          setSessions(sessions.filter((s) => s.status !== "closed"));
        }
      })
      .catch(() => {
        // No sessions yet, or the hub is unavailable — sidebar just shows
        // no Agent Hub badge, same as an empty session list.
      });
    return () => {
      mounted = false;
    };
  }, [setSessions]);

  useEffect(() => {
    return onAgentHubPermission((pending) => {
      const session = useAgentSessionsStore
        .getState()
        .sessions.find((s) => s.id === pending.sessionId);
      if (session) {
        upsertSession({ ...session, status: "awaiting_permission" });
      }
    });
  }, [upsertSession]);

  return null;
}
