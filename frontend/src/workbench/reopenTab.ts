import { toast } from "sonner";
import { getModule } from "@/modules/registry";
import {
  allOpenTabs,
  closedTabId,
  useWorkbenchTabsStore,
  type ClosedTab,
  type WorkbenchTabsState,
} from "@/stores/workbenchTabs";

/**
 * Reopening a closed tab, as ⌘⇧T, the tab menu and the palette all do it
 * (spec 2026-10-15-tab-strip-ux-design.md § 3).
 */

/**
 * The records worth offering, newest first, each with its index in the stack
 * (what reopenClosedTab takes). A record whose tab is open again — only a
 * preview keeps it; every other open forgets it — is left out: "reopening"
 * it would only activate a tab already on screen.
 */
export function offeredClosedTabs(
  state: Pick<WorkbenchTabsState, "recentlyClosed" | "modules">,
): { entry: ClosedTab; at: number }[] {
  const open = new Set(allOpenTabs(state.modules).map((tab) => tab.id));
  return state.recentlyClosed.flatMap((entry, at) => (open.has(closedTabId(entry)) ? [] : [{ entry, at }]));
}

/**
 * The modules Workbench found unusable — metadata that disagrees with Go, or
 * an API version this build does not speak (Workbench.tsx's
 * unavailableModuleIds). Registered still, but a tab opened in one shows
 * only that it is unavailable. Workbench keeps this current.
 */
let unavailableModules: ReadonlySet<string> = new Set();

export function setUnavailableModules(ids: ReadonlySet<string>): void {
  unavailableModules = ids;
}

/**
 * Reopen the stack's `at`th record. A record whose module or view is gone
 * since (turned off, unusable, or a newer build without that view) has
 * nothing to open in: it is dropped, and the person is told rather than
 * left with a press that did nothing.
 */
export function reopenClosedAt(at: number): string | null {
  const state = useWorkbenchTabsStore.getState();
  const entry = state.recentlyClosed[at];
  if (!entry) return null;
  const module = unavailableModules.has(entry.moduleId) ? undefined : getModule(entry.moduleId);
  if (!module?.views.some((view) => view.id === entry.viewId)) {
    useWorkbenchTabsStore.setState((s) => ({ recentlyClosed: s.recentlyClosed.filter((e) => e !== entry) }));
    toast.error(`${entry.title} can't be reopened`, { description: "Its module is no longer available." });
    return null;
  }
  return state.reopenClosedTab(at);
}

/** ⌘⇧T: the newest record that is not open again. */
export function reopenLastClosed(): string | null {
  const [newest] = offeredClosedTabs(useWorkbenchTabsStore.getState());
  return newest ? reopenClosedAt(newest.at) : null;
}
