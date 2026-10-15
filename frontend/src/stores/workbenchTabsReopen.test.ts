import { beforeEach, expect, test } from "bun:test";
import { persistedTabs, useWorkbenchTabsStore, type OpenTabInput } from "./workbenchTabs";

// Every setState below schedules a debounced ui_state write that lands 400ms
// later on a bridge with no Wails window — an unhandled rejection in whichever
// test file happens to be running by then.
useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

// Spec 2026-10-15-tab-strip-ux-design.md § 3: closed tabs reopen where they were.

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {}, groups: [], focusedGroupId: "", recentlyClosed: [] });
});

const open = (title: string, extra: Partial<OpenTabInput> = {}) =>
  useWorkbenchTabsStore
    .getState()
    .openTab({ moduleId: "notes", viewId: "notes.editor", title, params: { noteId: title }, ...extra });
const titles = () => useWorkbenchTabsStore.getState().modules.notes?.openTabs.map((t) => t.title) ?? [];

test("a closed tab comes back where it was", () => {
  const a = open("a");
  open("b");
  open("c");
  useWorkbenchTabsStore.getState().closeTab(a);
  expect(titles()).toEqual(["b", "c"]);
  const back = useWorkbenchTabsStore.getState().reopenClosedTab();
  expect(back).toBe(a);
  expect(titles()).toEqual(["a", "b", "c"]);
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(0);
});

test("reopen goes last-closed first and keeps the pin", () => {
  const a = open("a");
  const b = open("b");
  open("c");
  useWorkbenchTabsStore.getState().togglePinTab(a);
  useWorkbenchTabsStore.getState().closeTab(a);
  useWorkbenchTabsStore.getState().closeTab(b);
  useWorkbenchTabsStore.getState().reopenClosedTab();
  expect(titles()).toContain("b");
  expect(titles()).not.toContain("a");
  useWorkbenchTabsStore.getState().reopenClosedTab();
  const reopened = useWorkbenchTabsStore.getState().modules.notes.openTabs.find((t) => t.title === "a");
  expect(reopened?.pinned).toBe(true);
  expect(titles()).toEqual(["a", "b", "c"]);
});

test("preview tabs are not recorded; the stack is capped at 20", () => {
  const p = open("peek", { preview: true });
  useWorkbenchTabsStore.getState().closeTab(p);
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(0);
  for (let i = 0; i < 25; i++) useWorkbenchTabsStore.getState().closeTab(open(`t${i}`));
  const stack = useWorkbenchTabsStore.getState().recentlyClosed;
  expect(stack).toHaveLength(20);
  expect(stack[0].title).toBe("t24");
});

test("opening a tab again drops its record", () => {
  useWorkbenchTabsStore.getState().closeTab(open("a"));
  open("a");
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(0);
});

test("opening an unrelated tab keeps the stack as it was", () => {
  useWorkbenchTabsStore.getState().closeTab(open("a"));
  const before = useWorkbenchTabsStore.getState().recentlyClosed;
  open("b");
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toBe(before);
});

test("reopen picks a given entry, and returns null on an empty stack", () => {
  useWorkbenchTabsStore.getState().closeTab(open("a"));
  useWorkbenchTabsStore.getState().closeTab(open("b"));
  useWorkbenchTabsStore.getState().reopenClosedTab(1);
  expect(titles()).toEqual(["a"]);
  useWorkbenchTabsStore.getState().reopenClosedTab();
  expect(useWorkbenchTabsStore.getState().reopenClosedTab()).toBeNull();
});

test("a tab whose group is gone reopens in the focused group", () => {
  open("a");
  const b = open("b");
  useWorkbenchTabsStore.getState().splitTab(b);
  const groupB = useWorkbenchTabsStore.getState().groups.find((g) => g.activeTabId === b)!.id;
  useWorkbenchTabsStore.getState().closeTab(b); // its group empties and is dropped
  expect(useWorkbenchTabsStore.getState().groups.some((g) => g.id === groupB)).toBe(false);
  useWorkbenchTabsStore.getState().reopenClosedTab();
  const state = useWorkbenchTabsStore.getState();
  const tab = state.modules.notes.openTabs.find((t) => t.title === "b")!;
  expect(state.groups.some((g) => g.id === (tab.group ?? ""))).toBe(true);
  expect(tab.group).toBe(state.focusedGroupId);
});

test("a tab reopens in its own group when that group is still there", () => {
  open("a");
  const b = open("b");
  const c = open("c");
  useWorkbenchTabsStore.getState().splitTab(b);
  const groupB = useWorkbenchTabsStore.getState().focusedGroupId;
  useWorkbenchTabsStore.getState().moveTabToGroup(c, groupB);
  useWorkbenchTabsStore.getState().closeTab(b);
  // Work moves to the other group before the reopen.
  const other = useWorkbenchTabsStore.getState().groups.find((g) => g.id !== groupB)!.id;
  useWorkbenchTabsStore.getState().focusGroup(other);
  useWorkbenchTabsStore.getState().reopenClosedTab();
  const state = useWorkbenchTabsStore.getState();
  expect(state.modules.notes.openTabs.find((t) => t.id === b)?.group).toBe(groupB);
  expect(state.focusedGroupId).toBe(groupB);
});

test("a group close reopens one by one, last closed first", () => {
  const ids = [open("a"), open("b"), open("c")];
  // TabStrip's onCloseMany closes each tab through closeTab, in strip order.
  ids.forEach((id) => useWorkbenchTabsStore.getState().closeTab(id));
  expect(titles()).toEqual([]);
  useWorkbenchTabsStore.getState().reopenClosedTab();
  expect(titles()).toEqual(["c"]);
  useWorkbenchTabsStore.getState().reopenClosedTab();
  expect(titles()).toEqual(["b", "c"]);
  useWorkbenchTabsStore.getState().reopenClosedTab();
  expect(titles()).toEqual(["a", "b", "c"]);
});

test("the stack is not persisted", () => {
  useWorkbenchTabsStore.getState().closeTab(open("a"));
  const saved = persistedTabs(useWorkbenchTabsStore.getState());
  expect(Object.keys(saved)).not.toContain("recentlyClosed");
});
