import { beforeEach, describe, expect, test } from "bun:test";
import {
  EMPTY_MODULE_TABS,
  moduleOfTab,
  moduleTabs,
  selectActiveModuleTabs,
  useWorkbenchTabsStore,
} from "./workbenchTabs";

// Every setState below schedules a debounced ui_state write that lands 400ms
// later on a bridge with no Wails window — an unhandled rejection in whichever
// test file happens to be running by then.
useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

// module-tabs.mdx, "Design: per-module ownership": each module owns its tabs,
// state survives leaving a module, the Dock comes back to the module's own
// last-active tab, and none of the interaction rules change.

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {} });
});

describe("per-module tab ownership", () => {
  test("a tab id routes to its module without being told which", () => {
    expect(moduleOfTab("notes:notes.editor:noteId=n1")).toBe("notes");
    expect(moduleOfTab("agenthub:agenthub.sessions")).toBe("agenthub");
    // No separator at all is still a module id, not a crash.
    expect(moduleOfTab("ssh")).toBe("ssh");
  });

  test("opening a tab lands it in its module's partition and focuses that module", () => {
    const { openTab } = useWorkbenchTabsStore.getState();
    openTab({ moduleId: "notes", viewId: "notes.editor", title: "A", params: { noteId: "a" } });
    openTab({ moduleId: "ssh", viewId: "ssh.profiles", title: "SSH" });

    const { modules, activeModuleId } = useWorkbenchTabsStore.getState();
    expect(activeModuleId).toBe("ssh");
    expect(modules.notes.openTabs.map((t) => t.title)).toEqual(["A"]);
    expect(modules.ssh.openTabs.map((t) => t.title)).toEqual(["SSH"]);
    // Notes still knows which of ITS tabs is active, independent of SSH.
    expect(modules.notes.activeTabId).toBe("notes:notes.editor:noteId=a");
  });

  test("leaving a module and coming back finds its tabs exactly as left", () => {
    const store = useWorkbenchTabsStore.getState();
    store.openTab({ moduleId: "notes", viewId: "notes.editor", title: "A", params: { noteId: "a" } });
    store.openTab({ moduleId: "notes", viewId: "notes.editor", title: "B", params: { noteId: "b" } });
    store.setActiveTab("notes:notes.editor:noteId=a");

    store.focusModule("ssh");
    expect(useWorkbenchTabsStore.getState().activeModuleId).toBe("ssh");

    store.focusModule("notes");
    const notes = selectActiveModuleTabs(useWorkbenchTabsStore.getState());
    expect(notes.openTabs.map((t) => t.title)).toEqual(["A", "B"]);
    // Sticky: the tab that was active when we left, not the first or the last.
    expect(notes.activeTabId).toBe("notes:notes.editor:noteId=a");
  });

  test("the preview slot is per module, so the Dock never evicts another module's preview", () => {
    const { openTab } = useWorkbenchTabsStore.getState();
    openTab({ moduleId: "notes", viewId: "notes.editor", title: "A", params: { noteId: "a" }, preview: true });
    openTab({ moduleId: "ssh", viewId: "ssh.profiles", title: "SSH", preview: true });

    const { modules } = useWorkbenchTabsStore.getState();
    expect(modules.notes.openTabs.map((t) => t.title)).toEqual(["A"]);
    expect(modules.ssh.openTabs.map((t) => t.title)).toEqual(["SSH"]);

    // Within Notes the single-slot rule still holds: browsing replaces in place.
    openTab({ moduleId: "notes", viewId: "notes.editor", title: "C", params: { noteId: "c" }, preview: true });
    expect(useWorkbenchTabsStore.getState().modules.notes.openTabs.map((t) => t.title)).toEqual(["C"]);
  });

  test("closing the last tab empties the partition instead of switching modules", () => {
    const store = useWorkbenchTabsStore.getState();
    store.openTab({ moduleId: "ssh", viewId: "ssh.profiles", title: "SSH" });
    store.openTab({ moduleId: "notes", viewId: "notes.editor", title: "A", params: { noteId: "a" } });
    store.closeTab("notes:notes.editor:noteId=a");

    const state = useWorkbenchTabsStore.getState();
    expect(state.activeModuleId).toBe("notes");
    expect(state.modules.notes.openTabs).toEqual([]);
    expect(state.modules.notes.activeTabId).toBe("");
    // The other module is not consulted for a fallback and is not touched.
    expect(state.modules.ssh.openTabs).toHaveLength(1);
  });

  test("acting on one module publishes nothing for the others", () => {
    const store = useWorkbenchTabsStore.getState();
    store.openTab({ moduleId: "ssh", viewId: "ssh.profiles", title: "SSH" });
    store.openTab({ moduleId: "notes", viewId: "notes.editor", title: "A", params: { noteId: "a" } });
    const sshBefore = useWorkbenchTabsStore.getState().modules.ssh;

    store.setTabStatus("notes:notes.editor:noteId=a", "running");
    store.pinTab("notes:notes.editor:noteId=a");

    expect(useWorkbenchTabsStore.getState().modules.ssh).toBe(sshBefore);
  });

  test("a module that never opened anything reads as the frozen empty partition", () => {
    const { modules } = useWorkbenchTabsStore.getState();
    expect(moduleTabs(modules, "kafka")).toBe(EMPTY_MODULE_TABS);
    expect(Object.isFrozen(EMPTY_MODULE_TABS)).toBe(true);
  });
});
