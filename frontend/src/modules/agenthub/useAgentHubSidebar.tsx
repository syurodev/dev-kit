import { useMemo, useRef, useCallback } from "react";

import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { AgentSidebarTree } from "./surfaces";
import type { AgentSession } from "../../bridge/agenthub";

/**
 * Publishes the swarm tree into the shell rail, the same way Notes publishes
 * its folder rail (see modules/notes/useNotesSidebar).
 *
 * Agent Hub used to draw the swarm as a second rail *inside* its own content
 * card. Two vertical rails side by side whenever the shell sidebar was open,
 * and the shell rail itself sat empty because this module registered nothing —
 * `_Slot / Sidebar Content` in Figma is where a module's rail belongs.
 *
 * No module-local store here, unlike Notes: Notes registers from two different
 * views (dashboard and editor) so the rail's data has to outlive either one.
 * Agent Hub has a single view that already owns this state, so passing it in is
 * both smaller and harder to desynchronise.
 */
export function useAgentHubSidebar(args: {
  sessions: AgentSession[];
  activeSessionId?: string;
  pendingCounts: Record<string, number>;
  onSelectSession: (id: string) => void;
  onNewSession: () => void;
}) {
  const { sessions, activeSessionId, pendingCounts } = args;

  // The two callbacks are redefined every render by the view. Holding them in a
  // ref keeps them out of the memo below, so the rail re-registers when what it
  // *shows* changes rather than on every keystroke in the composer.
  const selectRef = useRef(args.onSelectSession);
  selectRef.current = args.onSelectSession;
  const newSessionRef = useRef(args.onNewSession);
  newSessionRef.current = args.onNewSession;

  const onSelectSession = useCallback((id: string) => selectRef.current(id), []);
  const onNewSession = useCallback(() => newSessionRef.current(), []);

  const content = useMemo(
    () => (
      <AgentSidebarTree
        sessions={sessions}
        activeSessionId={activeSessionId}
        pendingCounts={pendingCounts}
        onSelectSession={onSelectSession}
        onNewSession={onNewSession}
      />
    ),
    [sessions, activeSessionId, pendingCounts, onSelectSession, onNewSession],
  );

  useSidebarContent(content);
}
