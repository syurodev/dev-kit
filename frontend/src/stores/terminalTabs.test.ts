import { describe, expect, it } from "bun:test";
import { nextActiveKey, useTerminalTabsStore, type TerminalTab } from "./terminalTabs";

const tabs = (...keys: string[]): TerminalTab[] => keys.map((k) => ({ key: k, title: k }));

describe("nextActiveKey", () => {
  it("leaves the active tab alone when a background tab closes", () => {
    expect(nextActiveKey(tabs("a", "b", "c"), "b", "a")).toBe("b");
    expect(nextActiveKey(tabs("a", "b", "c"), "b", "c")).toBe("b");
  });

  it("moves to the tab on the right when the active one closes", () => {
    expect(nextActiveKey(tabs("a", "b", "c"), "b", "b")).toBe("c");
  });

  it("falls back to the left when the closed tab was last", () => {
    expect(nextActiveKey(tabs("a", "b", "c"), "c", "c")).toBe("b");
  });

  it("returns nothing when the last tab closes", () => {
    expect(nextActiveKey(tabs("a"), "a", "a")).toBe("");
  });
});

describe("useTerminalTabsStore", () => {
  it("never reuses a title after a close", () => {
    const s = useTerminalTabsStore.getState();
    useTerminalTabsStore.setState({ tabs: [], activeKey: "", nextId: 1 });
    s.openTab();
    s.openTab();
    const [first] = useTerminalTabsStore.getState().tabs;
    s.closeTab(first.key);
    s.openTab();

    const titles = useTerminalTabsStore.getState().tabs.map((t) => t.title);
    expect(new Set(titles).size).toBe(titles.length);
    expect(titles).toContain("Terminal 3");
  });

  it("focuses each newly opened tab", () => {
    useTerminalTabsStore.setState({ tabs: [], activeKey: "", nextId: 1 });
    const s = useTerminalTabsStore.getState();
    s.openTab();
    const firstKey = useTerminalTabsStore.getState().activeKey;
    s.openTab();
    expect(useTerminalTabsStore.getState().activeKey).not.toBe(firstKey);
  });
});
