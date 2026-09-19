import { beforeEach, describe, expect, test } from "bun:test";
import { tabsOfGroup } from "./documentGroups";
import { moduleTabs, useWorkbenchTabsStore } from "./workbenchTabs";

useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

const store = () => useWorkbenchTabsStore.getState();
const note = (id: string) => ({ moduleId: "notes", viewId: "notes.editor", title: id, params: { noteId: id } });
const noteId = (id: string) => `notes:notes.editor:noteId=${id}`;
const shown = () => store().groups.map((g) => `${g.moduleId}:${g.activeTabId.split("=")[1] ?? g.activeTabId}`);

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {}, groups: [], focusedGroupId: "" });
  store().focusModule("notes");
});

describe("document groups", () => {
  test("start as one group showing the module on screen", () => {
    store().openTab(note("a"));
    expect(shown()).toEqual(["notes:a"]);
    expect(store().activeModuleId).toBe("notes");
  });

  test("split right moves the tab into a new group beside its own", () => {
    store().openTab(note("a"));
    store().openTab(note("b"));
    store().splitTab(noteId("b"));

    expect(shown()).toEqual(["notes:a", "notes:b"]);
    expect(store().focusedGroupId).toBe(store().groups[1].id);
    // The focused group's tab is still what the module reads as active.
    expect(moduleTabs(store().modules, "notes").activeTabId).toBe(noteId("b"));
    // Each document lives in one group.
    const [left, right] = store().groups;
    expect(tabsOfGroup(store(), left).map((t) => t.title)).toEqual(["a"]);
    expect(tabsOfGroup(store(), right).map((t) => t.title)).toEqual(["b"]);
  });

  test("a group's only tab cannot be split away", () => {
    store().openTab(note("a"));
    const before = store();
    before.splitTab(noteId("a"));
    expect(store()).toBe(before);
  });

  test("focusing a group makes its tab the module's active one", () => {
    store().openTab(note("a"));
    store().openTab(note("b"));
    store().splitTab(noteId("b"));
    store().focusGroup(store().groups[0].id);
    expect(moduleTabs(store().modules, "notes").activeTabId).toBe(noteId("a"));
  });

  test("opening a note lands in the focused group of that module", () => {
    store().openTab(note("a"));
    store().openTab(note("b"));
    store().splitTab(noteId("b"));
    store().openTab(note("c"));
    const right = store().groups[1];
    expect(tabsOfGroup(store(), right).map((t) => t.title)).toEqual(["b", "c"]);
  });

  test("opening a note already open elsewhere focuses the group it is in", () => {
    store().openTab(note("a"));
    store().openTab(note("b"));
    store().splitTab(noteId("b"));
    store().openTab(note("a"));
    expect(store().focusedGroupId).toBe(store().groups[0].id);
    expect(store().groups).toHaveLength(2);
  });

  test("closing a group's last tab closes the group, never the last one", () => {
    store().openTab(note("a"));
    store().openTab(note("b"));
    store().splitTab(noteId("b"));
    store().closeTab(noteId("b"));
    expect(shown()).toEqual(["notes:a"]);
    store().closeTab(noteId("a"));
    expect(store().groups).toHaveLength(1);
  });

  test("each group has its own preview: browsing in one leaves the other's alone", () => {
    store().openTab({ ...note("a"), preview: true });
    store().openTab({ ...note("b"), preview: true, beside: true });
    expect(shown()).toEqual(["notes:a", "notes:b"]);
    // A third preview in the right group replaces b there, not a on the left.
    store().openTab({ ...note("c"), preview: true });
    expect(shown()).toEqual(["notes:a", "notes:c"]);
    expect(moduleTabs(store().modules, "notes").openTabs.map((t) => t.title)).toEqual(["a", "c"]);
  });

  test("a module opened beside keeps the other group as it was", () => {
    store().openTab(note("a"));
    store().openModuleBeside("codegraph");
    expect(store().groups.map((g) => g.moduleId)).toEqual(["notes", "codegraph"]);
    expect(store().activeModuleId).toBe("codegraph");
    // The module switcher focuses the group already showing a module.
    store().focusModule("notes");
    expect(store().focusedGroupId).toBe(store().groups[0].id);
  });

  test("the switcher changes the focused group's module when none shows it", () => {
    store().openTab(note("a"));
    store().openModuleBeside("codegraph");
    store().focusModule("database");
    expect(store().groups.map((g) => g.moduleId)).toEqual(["notes", "database"]);
  });

  test("moving a tab to the other group of its module", () => {
    store().openTab(note("a"));
    store().openTab(note("b"));
    store().openTab(note("c"));
    store().splitTab(noteId("c"));
    store().moveTabToGroup(noteId("b"), store().groups[1].id);
    const [left, right] = store().groups;
    expect(tabsOfGroup(store(), left).map((t) => t.title)).toEqual(["a"]);
    expect(tabsOfGroup(store(), right).map((t) => t.title)).toEqual(["b", "c"]);
    expect(right.activeTabId).toBe(noteId("b"));
  });

  test("no more than three groups", () => {
    for (const id of ["a", "b", "c", "d"]) store().openTab(note(id));
    store().splitTab(noteId("d"));
    store().focusGroup(store().groups[0].id);
    store().splitTab(noteId("c"));
    store().focusGroup(store().groups[0].id);
    store().splitTab(noteId("b"));
    expect(store().groups).toHaveLength(3);
  });

  test("closing a group sends its tabs back to another group of the module", () => {
    store().openTab(note("a"));
    store().openTab(note("b"));
    store().splitTab(noteId("b"));
    store().closeGroup(store().groups[1].id);
    expect(store().groups).toHaveLength(1);
    expect(tabsOfGroup(store(), store().groups[0]).map((t) => t.title)).toEqual(["a", "b"]);
  });
});

describe("opening beside", () => {
  test("puts a new tab in a new group right of the focused one", () => {
    store().openTab(note("a"));
    store().openTab({ ...note("b"), beside: true });
    expect(shown()).toEqual(["notes:a", "notes:b"]);
    expect(store().focusedGroupId).toBe(store().groups[1].id);
  });

  test("reuses another group of the module when there is one", () => {
    store().openTab(note("a"));
    store().openTab({ ...note("b"), beside: true });
    store().focusGroup(store().groups[0].id);
    store().openTab({ ...note("c"), beside: true });
    expect(store().groups).toHaveLength(2);
    expect(tabsOfGroup(store(), store().groups[1]).map((t) => t.title)).toEqual(["b", "c"]);
  });

  test("a document already open is focused where it is, not duplicated", () => {
    store().openTab(note("a"));
    store().openTab({ ...note("a"), beside: true });
    expect(store().groups).toHaveLength(1);
  });

  test("a ⌘-click armed by the sidebar counts as beside", async () => {
    const { armBeside } = await import("./workbenchTabs");
    store().openTab(note("a"));
    armBeside();
    store().openTab(note("b"));
    expect(store().groups).toHaveLength(2);
  });
});
