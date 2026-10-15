import { create } from "zustand";
import { shownTab } from "@/stores/documentGroups";
import { allOpenTabs, useWorkbenchTabsStore, type WorkbenchTabsState } from "@/stores/workbenchTabs";
import { nextSeen } from "./tabStripModel";

/**
 * Finished tabs that have been looked at, so their success dot is spent
 * (spec 2026-10-15-tab-strip-ux-design.md § 2). Display state, not the
 * store's: the module's status stays what it said.
 *
 * Followed from the tabs store itself rather than kept by a strip: a strip
 * unmounts when its group closes or shows another module, and a tab moves
 * between groups, and what was seen must outlive all three. "Seen" is any
 * group's shown tab while it is finished.
 */
export const useSeenSuccessStore = create<{ seen: ReadonlySet<string> }>(() => ({ seen: new Set<string>() }));

function follow(state: WorkbenchTabsState) {
  const { seen } = useSeenSuccessStore.getState();
  const next = nextSeen(seen, allOpenTabs(state.modules), state.groups.map((group) => shownTab(state, group)));
  if (next !== seen) useSeenSuccessStore.setState({ seen: next });
}

follow(useWorkbenchTabsStore.getState());
useWorkbenchTabsStore.subscribe(follow);
