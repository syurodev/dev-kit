import { useEffect } from "react";
import {
  agentHubGetStatus,
  agentHubListAgents,
  agentHubPendingPermissions,
  onAgentHubPermission,
  onAgentHubSessionUpdate,
} from "../bridge/agenthub";
import { appendUpdateLine, useAgentSessionsStore } from "../stores/agentSessions";
import { useWorkbenchTabsStore, type TabStatus } from "../stores/workbenchTabs";
import { UpdateKind } from "../../bindings/devkit/internal/agenthub/models";

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
   * The transcript stream & tab status synchronization.
   * `agenthub:sessionUpdate` → `setTabStatus`
   *
   * Subscribed globally so turns continue streaming when working in another module.
   * Synchronizes tab status with ACP update stream ('running' during turns, 'error' on failure).
   * Tasks will be the second consumer of setTabStatus.
   */
  useEffect(() => {
    return onAgentHubSessionUpdate((event) => {
      updateLines(event.sessionId, (prev) => appendUpdateLine(prev, event.update));

      // The agent names the session itself, after the first exchange, from
      // what was actually said. Until this landed every row in the swarm tree
      // read "Session 1d5b9d50" — an id is not a name. The backend has already
      // persisted it by the time this update arrives; this is what renames the
      // row without waiting for a reload.
      if (event.parsed?.kind === UpdateKind.UpdateSessionInfo && event.parsed.title) {
        const { sessions, upsertSession } = useAgentSessionsStore.getState();
        const session = sessions.find((s) => s.id === event.sessionId);
        if (session && !session.title) {
          upsertSession({ ...session, title: event.parsed.title });
        }
      }

      // Synchronize tab status: find open tabs belonging to Agent Hub
      const { openTabs, setTabStatus } = useWorkbenchTabsStore.getState();
      const targetTabs = openTabs.filter(
        (t) =>
          t.moduleId === "agenthub" &&
          (!t.params?.sessionId || t.params.sessionId === event.sessionId),
      );

      if (targetTabs.length > 0) {
        let nextStatus: TabStatus = "running";
        if (event.parsed) {
          const kind = event.parsed.kind;
          if (
            kind === UpdateKind.UpdateToolCallUpdate &&
            event.parsed.toolCall?.status === "failed"
          ) {
            nextStatus = "error";
          }
        }
        for (const tab of targetTabs) {
          setTabStatus(tab.id, nextStatus);
        }
      }
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
