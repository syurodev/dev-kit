import "@/testing/happyDom";
import { afterEach, beforeEach, expect, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import type { AgentSession } from "@/bridge/agenthub";
import { SidebarModuleIdContext } from "@/components/ui/sidebar-shell";
import { SplitPanelProvider, useSplitPanelState } from "@/components/ui/split-panel";
import { useAgentPinsStore } from "@/stores/agentPins";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { useSubagentPanel } from "../subagents";
import { useAgentHubEffects } from "./useAgentHubEffects";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

useAgentPinsStore.persist.setOptions({ storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });

const session = { id: "s1", agentType: "claude", projectPath: "/p", status: "idle", parentSessionId: null, depth: 0, title: "Fix login", acpSessionId: "" } as AgentSession;

let seen: ReturnType<typeof useSplitPanelState>["panels"] = {};

// A pane opened again on every render would loop (openPanel is a store write, the store a re-render): count
// them and fail fast, with a message, rather than spin. Each openPanel makes a new entry in the store.
let renders = 0;
let opens = 0;
let lastEntry: unknown;
function Tab() {
  if (++renders > 60) throw new Error("the tab re-renders without end: a header or a dependency changes on every render");
  useAgentHubEffects({
    tabSessionId: "s1",
    reads: { visible: true, activeSession: session } as never,
    draft: {} as never,
    actions: {} as never,
  });
  return null;
}
function Spy() {
  seen = useSplitPanelState().panels;
  if (seen.agenthub !== lastEntry) {
    lastEntry = seen.agenthub;
    if (++opens > 50) throw new Error("the pane is opened again and again: its header or a dependency changes on every render");
  }
  return null;
}

let container: HTMLDivElement;
let root: Root;
beforeEach(async () => {
  renders = 0;
  opens = 0;
  lastEntry = undefined;
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  useAgentSessionsStore.setState({ sessionsById: { s1: session }, subagents: {}, transcriptsById: {} as never });
  await act(async () =>
    root.render(
      <QueryClientProvider client={new QueryClient()}>
        <SplitPanelProvider>
          <SidebarModuleIdContext.Provider value="agenthub">
            <Tab />
          </SidebarModuleIdContext.Provider>
          <Spy />
        </SplitPanelProvider>
      </QueryClientProvider>,
    ),
  );
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  useSubagentPanel.setState({ open: null });
  useAgentPinsStore.setState({ following: [] });
  useAgentSessionsStore.setState({ sessionsById: {}, subagents: {}, transcriptsById: {} as never });
});

test("a subagent opened from the sidebar goes in the pane with the subagent's header; its name arriving updates the header, not the body", async () => {
  await act(async () => useSubagentPanel.getState().show("s1", "a1"));
  const opened = seen.agenthub;
  expect(opened.header?.title).toBe("Subagent");
  expect(opened.key).toBe("subagent:s1:a1");
  expect(opens).toBeLessThanOrEqual(3);

  await act(async () => useAgentSessionsStore.setState({ subagents: { s1: [{ id: "a1", name: "Review whitelist", task: "Check the list" }] } }));
  const named = seen.agenthub;
  expect([named.header?.title, named.header?.titleHint]).toEqual(["Review whitelist", "Review whitelist\nCheck the list"]);
  expect(opens).toBeLessThanOrEqual(5);
  // The same tree: the shell keys the body by generation.
  expect(named.generation).toBe(opened.generation);
  expect(named.key).toBe(opened.key);

  // A state change is the header's own business: nothing is reopened.
  await act(async () => useAgentSessionsStore.setState({ subagents: { s1: [{ id: "a1", name: "Review whitelist", task: "Check the list", state: "completed" }] } }));
  expect(Object.is(seen.agenthub.header, named.header)).toBe(true);
  expect(opens).toBeLessThanOrEqual(5);
  expect(renders).toBeLessThanOrEqual(12);
});

test("the shell's close of a subagent pane closes the subagent pane, and the pane goes with it", async () => {
  await act(async () => useSubagentPanel.getState().show("s1", "a1"));
  expect(useSubagentPanel.getState().open).toEqual({ sessionId: "s1", subagentId: "a1" });
  await act(async () => seen.agenthub.onClose());
  expect(useSubagentPanel.getState().open).toBeNull();
  expect(seen.agenthub).toBeUndefined();
});

test("following a session puts the feed in the pane with the session's title; stopping following is its action and its close", async () => {
  await act(async () => useAgentPinsStore.getState().toggleFollowing("s1"));
  const feed = seen.agenthub;
  expect(feed.key).toBe("activity:s1");
  expect(feed.header?.title).toBe("Following Fix login");
  expect(feed.header?.actions).toBeDefined();
  expect(opens).toBeLessThanOrEqual(3);
  // The tab rendering again for a reason of its own (another session's subagent opened) reopens nothing.
  await act(async () => useSubagentPanel.getState().show("other", "x"));
  expect(renders).toBeGreaterThan(1);
  expect(Object.is(seen.agenthub, feed)).toBe(true);
  await act(async () => useSubagentPanel.getState().close());
  await act(async () => feed.onClose());
  expect(useAgentPinsStore.getState().following).toEqual([]);
});
