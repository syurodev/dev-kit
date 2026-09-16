import { useMemo, useRef, useCallback } from "react";

import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { AgentSidebarTree } from "./surfaces";
import { useAgentPinsStore } from "../../stores/agentPins";
import { useAgentSessionsStore } from "../../stores/agentSessions";
import type { AgentSession } from "../../bridge/agenthub";

/**
 * Publishes the swarm tree into the shell rail, the same way Notes publishes
 * its folder rail (see modules/notes/useNotesSidebar).
 *
 * The swarm belongs in the shell rail (`_Slot / Sidebar Content` in Figma), not
 * in a second rail inside the content card: that leaves two vertical rails side
 * by side whenever the shell sidebar is open, and the shell rail empty.
 *
 * No module-local store here, unlike Notes: Notes registers from two different
 * views (dashboard and editor) so the rail's data has to outlive either one.
 * Agent Hub has a single view that already owns this state, so passing it in is
 * both smaller and harder to desynchronise.
 *
 * Pins are the exception, and they come from stores/agentPins rather than from
 * the view: a pin outlives the session list, survives a restart, and no view
 * state is involved in reading it.
 */
export function useAgentHubSidebar(args: {
  sessions: AgentSession[];
  activeSessionId?: string;
  pendingCounts: Record<string, number>;
  onSelectSession: (id: string) => void;
  onNewSession: () => void;
  onNewSessionIn: (projectPath: string) => void;
  onNewChildSession: (parent: AgentSession) => void;
  onCancelTurn: (session: AgentSession) => void;
  onCloseSession: (session: AgentSession) => void;
  onDeleteSession: (session: AgentSession, descendants: number) => void;
  onCopy: (text: string) => void;
}) {
  const { sessions, activeSessionId, pendingCounts } = args;
  const pinned = useAgentPinsStore((state) => state.pinned);
  const togglePin = useAgentPinsStore((state) => state.togglePin);
  const collapsed = useAgentPinsStore((state) => state.collapsed);
  const toggleCollapsed = useAgentPinsStore((state) => state.toggleCollapsed);
  const hideClosed = useAgentPinsStore((state) => state.hideClosed);
  const toggleHideClosed = useAgentPinsStore((state) => state.toggleHideClosed);
  // Every session's subagents, not the open one's: the rail draws them all.
  const subagents = useAgentSessionsStore((state) => state.subagents);

  // The callbacks are redefined every render by the view. Holding them in a ref
  // keeps them out of the memo below, so the rail re-registers when what it
  // *shows* changes rather than on every keystroke in the composer.
  const handlers = useRef(args);
  handlers.current = args;

  const onSelectSession = useCallback(
    (id: string) => handlers.current.onSelectSession(id),
    [],
  );
  const onNewSession = useCallback(() => handlers.current.onNewSession(), []);
  const onNewSessionIn = useCallback(
    (projectPath: string) => handlers.current.onNewSessionIn(projectPath),
    [],
  );
  const onNewChildSession = useCallback(
    (parent: AgentSession) => handlers.current.onNewChildSession(parent),
    [],
  );
  const onCancelTurn = useCallback(
    (session: AgentSession) => handlers.current.onCancelTurn(session),
    [],
  );
  const onCloseSession = useCallback(
    (session: AgentSession) => handlers.current.onCloseSession(session),
    [],
  );
  const onDeleteSession = useCallback(
    (session: AgentSession, descendants: number) =>
      handlers.current.onDeleteSession(session, descendants),
    [],
  );
  const onCopy = useCallback((text: string) => handlers.current.onCopy(text), []);

  const content = useMemo(
    () => (
      <AgentSidebarTree
        sessions={sessions}
        activeSessionId={activeSessionId}
        pendingCounts={pendingCounts}
        subagents={subagents}
        pinnedIds={pinned}
        onTogglePin={togglePin}
        collapsedKeys={collapsed}
        onToggleCollapsed={toggleCollapsed}
        hideClosed={hideClosed}
        onToggleHideClosed={toggleHideClosed}
        onSelectSession={onSelectSession}
        onNewSession={onNewSession}
        onNewSessionIn={onNewSessionIn}
        onNewChildSession={onNewChildSession}
        onCancelTurn={onCancelTurn}
        onCloseSession={onCloseSession}
        onDeleteSession={onDeleteSession}
        onCopy={onCopy}
      />
    ),
    [
      sessions,
      activeSessionId,
      pendingCounts,
      subagents,
      pinned,
      togglePin,
      collapsed,
      toggleCollapsed,
      hideClosed,
      toggleHideClosed,
      onSelectSession,
      onNewSession,
      onNewSessionIn,
      onNewChildSession,
      onCancelTurn,
      onCloseSession,
      onDeleteSession,
      onCopy,
    ],
  );

  useSidebarContent(content);
}
