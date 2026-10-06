import { lazy, Suspense, useEffect } from "react";
import { useSplitPanel } from "@/components/ui/split-panel";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";

const LazyAgentCompanion = lazy(() =>
  import("./AgentCompanion").then((m) => ({ default: m.AgentCompanion })),
);

/** Whether the Agent panel may be shown while this module is on screen. */
export function agentPanelAllowed(activeModuleId: string | null | undefined): boolean {
  return activeModuleId !== "agenthub";
}

/**
 * Publishes the conversation into the shell's companion slot.
 *
 * A component rather than an inline effect in Workbench so that its only
 * dependency is the toggle — Workbench re-renders on plenty of unrelated state,
 * and re-pushing panel content on each of those would remount the transcript.
 * Code-split via React.lazy so Agent Hub surfaces and leaves do not inflate
 * the initial shell JS budget.
 *
 * Never inside Agent Hub: there every session is a tab of the module itself,
 * and a second copy of the conversation took the column the module's own
 * panels (a diff, a file the agent changed, a query result) need. `chatOpen`
 * is left as the person set it, so the panel is back when they leave.
 */
export function AgentCompanionSlot() {
  const chatOpen = useAgentSessionsStore((s) => s.chatOpen);
  const inAgentHub = useWorkbenchTabsStore((s) => !agentPanelAllowed(s.activeModuleId));
  const setChatOpen = useAgentSessionsStore((s) => s.setChatOpen);
  const { openCompanion, closeCompanion } = useSplitPanel();

  useEffect(() => {
    if (!chatOpen || inAgentHub) {
      closeCompanion();
      return;
    }
    openCompanion(
      <Suspense fallback={null}>
        <LazyAgentCompanion visible={chatOpen} />
      </Suspense>,
      { onClose: () => setChatOpen(false) },
    );
    return () => closeCompanion();
  }, [chatOpen, inAgentHub, openCompanion, closeCompanion, setChatOpen]);

  return null;
}
