import { describe, expect, test } from "bun:test";
import type { WorkbenchTab } from "@/stores/workbenchTabs";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { haveSameTabMembers } from "./workbenchViewMemo";

// No ui_state writes: the debounced bridge call would reject after this file ends.
useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

const first: WorkbenchTab = {
  id: "notes:edit:id=first",
  moduleId: "notes",
  viewId: "edit",
  title: "First",
};
const second: WorkbenchTab = {
  id: "notes:edit:id=second",
  moduleId: "notes",
  viewId: "edit",
  title: "Second",
};

describe("workbench tab reorder isolation", () => {
  test("treats a pure reorder as the same mounted content members", () => {
    expect(haveSameTabMembers([first, second], [second, first])).toBe(true);
  });

  test("invalidates mounted content when a tab object really changes", () => {
    expect(
      haveSameTabMembers([first, second], [second, { ...first, title: "Renamed" }]),
    ).toBe(false);
  });

  test("does not publish a new store state for an order no-op", () => {
    useWorkbenchTabsStore.setState({
      activeModuleId: "notes",
      modules: {
        notes: {
          openTabs: [first, second],
          activeTabId: first.id,
          tabMru: [first.id, second.id],
        },
      },
    });
    const before = useWorkbenchTabsStore.getState();

    before.reorderTab(first.id, second.id);

    expect(useWorkbenchTabsStore.getState()).toBe(before);
  });

  test("preserves tab object identity for a real reorder", () => {
    useWorkbenchTabsStore.setState({
      activeModuleId: "notes",
      modules: {
        notes: {
          openTabs: [first, second],
          activeTabId: first.id,
          tabMru: [first.id, second.id],
        },
      },
    });

    useWorkbenchTabsStore.getState().reorderTab(first.id, null);

    const { openTabs } = useWorkbenchTabsStore.getState().modules.notes;
    expect(openTabs).toEqual([second, first]);
    expect(openTabs[1]).toBe(first);
  });

  test("does not publish state when the already-active tab is selected", () => {
    useWorkbenchTabsStore.setState({
      activeModuleId: "notes",
      modules: {
        notes: {
          openTabs: [first, second],
          activeTabId: first.id,
          tabMru: [first.id, second.id],
        },
      },
    });
    const before = useWorkbenchTabsStore.getState();

    before.setActiveTab(first.id);

    expect(useWorkbenchTabsStore.getState()).toBe(before);
  });
});
