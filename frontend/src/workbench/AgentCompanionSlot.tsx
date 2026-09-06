import { lazy, Suspense, useEffect } from "react";
import { useSplitPanel } from "@/components/ui/split-panel";
import { useAgentSessionsStore } from "@/stores/agentSessions";

const LazyAgentCompanion = lazy(() =>
  import("./AgentCompanion").then((m) => ({ default: m.AgentCompanion })),
);

/**
 * Publishes the conversation into the shell's companion slot.
 *
 * A component rather than an inline effect in Workbench so that its only
 * dependency is the toggle — Workbench re-renders on plenty of unrelated state,
 * and re-pushing panel content on each of those would remount the transcript.
 * Code-split via React.lazy so Agent Hub surfaces and leaves do not inflate
 * the initial shell JS budget.
 */
export function AgentCompanionSlot() {
  const chatOpen = useAgentSessionsStore((s) => s.chatOpen);
  const setChatOpen = useAgentSessionsStore((s) => s.setChatOpen);
  const { openCompanion, closeCompanion } = useSplitPanel();

  useEffect(() => {
    if (!chatOpen) {
      closeCompanion();
      return;
    }
    openCompanion(
      <Suspense fallback={null}>
        <LazyAgentCompanion />
      </Suspense>,
      { onClose: () => setChatOpen(false) },
    );
    return () => closeCompanion();
  }, [chatOpen, openCompanion, closeCompanion, setChatOpen]);

  return null;
}
