import { lazy, Suspense, useEffect, useState } from "react";
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
 * Whether the companion is mounted, and what to push: nothing until it is
 * first shown, then always — visible or hidden.
 */
export function companionPush(s: { chatOpen: boolean; inAgentHub: boolean; mounted: boolean }): {
  mounted: boolean;
  push: { hidden: boolean } | null;
} {
  const shown = s.chatOpen && !s.inAgentHub;
  const mounted = s.mounted || shown;
  return { mounted, push: mounted ? { hidden: !shown } : null };
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
 * Kept alive once opened: closing the pane hides the conversation rather than
 * unmounting it, so reopening is instant and finds the transcript where it was
 * scrolled and the composer as it was left (its attachments are component
 * state). Hidden, it is `visible={false}`: it reads the transcript the store
 * holds without subscribing to it, fetches nothing and raises no follow toasts.
 * Not mounted at all until it is first opened, so a session that never opens
 * it never loads the chunk.
 *
 * Never shown inside Agent Hub: there every session is a tab of the module
 * itself, and a second copy of the conversation took the column the module's
 * own panels (a diff, a file the agent changed, a query result) need. `chatOpen`
 * is left as the person set it, so the panel is back when they leave.
 */
export function AgentCompanionSlot() {
  const chatOpen = useAgentSessionsStore((s) => s.chatOpen);
  const inAgentHub = useWorkbenchTabsStore((s) => !agentPanelAllowed(s.activeModuleId));
  const setChatOpen = useAgentSessionsStore((s) => s.setChatOpen);
  const { openCompanion, closeCompanion } = useSplitPanel();
  const [wasMounted, setMounted] = useState(false);
  const { mounted, push } = companionPush({ chatOpen, inAgentHub, mounted: wasMounted });
  if (mounted && !wasMounted) setMounted(true);
  const hidden = push?.hidden ?? true;

  useEffect(() => {
    if (!mounted) return;
    openCompanion(
      <Suspense fallback={null}>
        <LazyAgentCompanion visible={!hidden} />
      </Suspense>,
      { onClose: () => setChatOpen(false), hidden },
    );
  }, [mounted, hidden, openCompanion, setChatOpen]);

  // Only on the way out of the shell: a toggle re-pushes, it never clears.
  useEffect(() => () => closeCompanion(), [closeCompanion]);

  return null;
}
