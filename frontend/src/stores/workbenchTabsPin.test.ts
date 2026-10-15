import { beforeEach, describe, expect, test } from "bun:test";
import { moduleTabs, useWorkbenchTabsStore } from "./workbenchTabs";

// No ui_state writes: the debounced bridge call would reject after this file ends.
useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {}, groups: [], focusedGroupId: "", recentlyClosed: [] });
});

const open = (title: string) =>
  useWorkbenchTabsStore
    .getState()
    .openTab({ moduleId: "notes", viewId: "notes.editor", title, params: { noteId: title } });

const titles = () =>
  moduleTabs(useWorkbenchTabsStore.getState().modules, "notes").openTabs.map(
    (tab) => `${tab.title}${tab.pinned ? "*" : ""}`,
  );

describe("pinned tabs", () => {
  test("pinning moves a tab to the end of the pinned run", () => {
    const ids = ["a", "b", "c", "d"].map(open);
    const { togglePinTab } = useWorkbenchTabsStore.getState();
    togglePinTab(ids[2]);
    expect(titles()).toEqual(["c*", "a", "b", "d"]);
    togglePinTab(ids[3]);
    expect(titles()).toEqual(["c*", "d*", "a", "b"]);
  });

  test("unpinning puts it first among the unpinned", () => {
    const ids = ["a", "b", "c"].map(open);
    const { togglePinTab } = useWorkbenchTabsStore.getState();
    togglePinTab(ids[1]);
    togglePinTab(ids[2]);
    togglePinTab(ids[1]);
    expect(titles()).toEqual(["c*", "b", "a"]);
  });

  test("a drag cannot put an unpinned tab in front of a pinned one", () => {
    const ids = ["a", "b", "c"].map(open);
    const { togglePinTab, reorderTab } = useWorkbenchTabsStore.getState();
    togglePinTab(ids[0]);
    reorderTab(ids[2], ids[0]);
    expect(titles()).toEqual(["a*", "c", "b"]);
  });

  test("a pinned preview tab stops being the preview", () => {
    const id = useWorkbenchTabsStore
      .getState()
      .openTab({ moduleId: "notes", viewId: "notes.editor", title: "p", params: { noteId: "p" }, preview: true });
    useWorkbenchTabsStore.getState().togglePinTab(id);
    const tab = moduleTabs(useWorkbenchTabsStore.getState().modules, "notes").openTabs[0];
    expect(tab.pinned).toBe(true);
    expect(tab.preview).toBe(false);
  });
});
