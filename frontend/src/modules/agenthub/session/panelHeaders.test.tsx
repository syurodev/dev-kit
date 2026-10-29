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

function Tab() {
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
  return null;
}

let container: HTMLDivElement;
let root: Root;
beforeEach(async () => {
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

  await act(async () => useAgentSessionsStore.setState({ subagents: { s1: [{ id: "a1", name: "Review whitelist", task: "Check the list" }] } }));
  const named = seen.agenthub;
  expect(named.header).toMatchObject({ title: "Review whitelist", titleHint: "Check the list" });
  // The same tree: the shell keys the body by generation.
  expect(named.generation).toBe(opened.generation);
  expect(named.key).toBe(opened.key);

  // A state change is the header's own business: nothing is reopened.
  await act(async () => useAgentSessionsStore.setState({ subagents: { s1: [{ id: "a1", name: "Review whitelist", task: "Check the list", state: "completed" }] } }));
  expect(seen.agenthub.header).toBe(named.header);
});

test("following a session puts the feed in the pane with the session's title; stopping following is its action and its close", async () => {
  await act(async () => useAgentPinsStore.getState().toggleFollowing("s1"));
  const feed = seen.agenthub;
  expect(feed.key).toBe("activity:s1");
  expect(feed.header?.title).toBe("Fix login");
  expect(feed.header?.actions).toBeDefined();
  await act(async () => feed.onClose());
  expect(useAgentPinsStore.getState().following).toEqual([]);
});
