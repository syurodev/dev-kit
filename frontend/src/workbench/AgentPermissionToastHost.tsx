import { useState } from "react";
import {
  agentHubGetStatus,
  agentHubResolvePermission,
  cancelledPermissionOutcome,
  selectedPermissionOutcome,
  type AgentSession,
  type PendingPermission,
} from "@/bridge/agenthub";
import { AgentPermissionToast } from "@/modules/agenthub/surfaces/AgentPermissionToast";
import { useAgentSessionList, useAgentSessionsStore } from "@/stores/agentSessions";

/**
 * Hosts the Agent / Permission Toast (Figma 924:152) at the shell level.
 *
 * Appears floating bottom-right over any module when:
 * - A permission request is pending from an agent, AND
 * - The rail is closed (!chatOpen) OR another module is active and the rail is
 *   focused on a different session.
 *
 * "Open rail" reaches the full transcript before granting anything persistent;
 * "Allow once" approves a single turn.
 */
export type AgentPermissionToastHostProps = {
  activeModuleId?: string | null;
  pending?: PendingPermission | null;
  session?: AgentSession | null;
  chatOpen?: boolean;
};

export function AgentPermissionToastHost({
  activeModuleId,
  pending: propPending,
  session: propSession,
  chatOpen: propChatOpen,
}: AgentPermissionToastHostProps = {}) {
  const storePending = useAgentSessionsStore((s) => s.pending);
  const dequeuePending = useAgentSessionsStore((s) => s.dequeuePending);
  const storeSessions = useAgentSessionList();
  const storeActiveSessionId = useAgentSessionsStore((s) => s.activeSessionId);
  const storeChatOpen = useAgentSessionsStore((s) => s.chatOpen);
  const setChatOpen = useAgentSessionsStore((s) => s.setChatOpen);
  const setActiveSessionId = useAgentSessionsStore((s) => s.setActiveSessionId);
  const upsertSession = useAgentSessionsStore((s) => s.upsertSession);

  const [busy, setBusy] = useState(false);

  // In SSR / bun test without DOM window, Zustand getServerSnapshot returns immutable initial state,
  // so we fallback to getState() when window is undefined.
  const isSSR = typeof window === "undefined";
  const pending =
    propPending !== undefined
      ? propPending
      : isSSR
        ? useAgentSessionsStore.getState().pending
        : storePending;
  const sessions = isSSR
    ? useAgentSessionsStore.getState().sessionIds.map((id) => useAgentSessionsStore.getState().sessionsById[id])
    : storeSessions;
  const activeSessionId = isSSR ? useAgentSessionsStore.getState().activeSessionId : storeActiveSessionId;
  const chatOpen =
    propChatOpen !== undefined
      ? propChatOpen
      : isSSR
        ? useAgentSessionsStore.getState().chatOpen
        : storeChatOpen;

  if (!pending) return null;

  // The toast floats over any module when the rail is closed or when
  // another session/module is active without showing this pending request inline.
  const showToast =
    !chatOpen || (activeModuleId !== "agenthub" && activeSessionId !== pending.sessionId);
  if (!showToast) return null;

  const session = propSession ?? sessions.find((s) => s.id === pending.sessionId) ?? null;

  async function handleResolve(optionId: string | null) {
    if (!pending) return;
    setBusy(true);
    try {
      await agentHubResolvePermission(
        pending.id,
        optionId === null
          ? cancelledPermissionOutcome()
          : selectedPermissionOutcome(optionId),
      );
      dequeuePending(pending.id);
      try {
        const updated = await agentHubGetStatus(pending.sessionId);
        upsertSession(updated);
      } catch {
        // Session might be gone or hub unavailable
      }
    } finally {
      setBusy(false);
    }
  }

  function handleOpenRail() {
    if (pending) {
      setActiveSessionId(pending.sessionId);
    }
    setChatOpen(true);
  }

  return (
    // Placement is the host's job, not the card's. Figma 924:152 draws a 420
    // card; "bottom-right, floating over the module" is in its description, and
    // a card that positions itself cannot be put anywhere else — including the
    // gallery, where it would fly to the corner of the page.
    <div className="pointer-events-none fixed right-4 bottom-4 z-50">
      <div className="pointer-events-auto">
        <AgentPermissionToast
          pending={pending}
          session={session}
          onResolve={handleResolve}
          onOpenRail={handleOpenRail}
          busy={busy}
        />
      </div>
    </div>
  );
}
