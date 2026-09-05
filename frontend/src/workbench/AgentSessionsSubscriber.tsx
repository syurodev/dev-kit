import { useEffect } from "react";
import {
  agentHubGetStatus,
  agentHubListAgents,
  agentHubPendingPermissions,
  onAgentHubPermission,
  onAgentHubSessionUpdate,
} from "../bridge/agenthub";
import { appendUpdateLine, useAgentSessionsStore } from "../stores/agentSessions";

/**
 * Keeps the shared Agent Hub session store in sync regardless of which
 * module is mounted. Rendered once in Workbench.tsx, as a sibling of the
 * sidebar and content card — never remounted on module switch, unlike
 * AgentHubView which only exists while that module is active.
 */
export function AgentSessionsSubscriber() {
  const setSessions = useAgentSessionsStore((state) => state.setSessions);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);
  const updateLines = useAgentSessionsStore((state) => state.updateLines);
  const enqueuePending = useAgentSessionsStore((state) => state.enqueuePending);

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
    agentHubPendingPermissions()
      .then((list) => {
        if (mounted) {
          list.forEach((p) => enqueuePending(p));
        }
      })
      .catch(() => {
        // Hub unavailable — no prompts to restore.
      });
    return () => {
      mounted = false;
    };
  }, [setSessions, enqueuePending]);

  /**
   * The transcript stream. This used to be subscribed inside AgentHubView,
   * which tore it down on every module switch: an agent that kept working
   * while you looked at something else streamed into a handler nobody had
   * registered, and those turns were lost outright. It belongs here, beside
   * the session sync, for exactly the reason in this file's header.
   */
  useEffect(() => {
    return onAgentHubSessionUpdate((event) => {
      updateLines(event.sessionId, (prev) => appendUpdateLine(prev, event.update));
    });
  }, [updateLines]);

  useEffect(() => {
    return onAgentHubPermission(async (pending) => {
      enqueuePending(pending);
      const session = useAgentSessionsStore
        .getState()
        .sessions.find((s) => s.id === pending.sessionId);
      if (session) {
        upsertSession({ ...session, status: "awaiting_permission" });
        return;
      }
      // Session isn't in the store yet — the initial list load is likely
      // still in flight. Fetch it directly so the permission event isn't
      // silently dropped.
      try {
        const fetched = await agentHubGetStatus(pending.sessionId);
        upsertSession({ ...fetched, status: "awaiting_permission" });
      } catch {
        // Hub unavailable or session gone — nothing to show.
      }
    });
  }, [upsertSession, enqueuePending]);

  return null;
}
