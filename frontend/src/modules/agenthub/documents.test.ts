import { beforeEach, expect, test } from "bun:test";
import type { AgentSession } from "@/bridge/agenthub";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import {
  activeAgentHubSessionId,
  addAgentSessionTabInBackground,
  agentSessionOnScreen,
  agentSessionTabs,
  closeAgentSessionTabs,
  closeNewSessionTab,
  openAgentSession,
  openNewAgentSession,
  showAgentSession,
  syncAgentSessionTabTitles,
  useAgentDraftRequests,
} from "./documents";

useWorkbenchTabsStore.persist.setOptions({ storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });

function session(id: string, title: string | null = `Title ${id}`): AgentSession {
  return { id, agentType: "claude", projectPath: "/repo", status: "idle", parentSessionId: null, depth: 0, title, acpSessionId: "" };
}

const tabs = () => moduleTabs(useWorkbenchTabsStore.getState().modules, "agenthub");

beforeEach(() => {
  useWorkbenchTabsStore.setState({ modules: {}, groups: [], focusedGroupId: "", activeModuleId: "notes" });
  useAgentSessionsStore.setState({ sessionsById: {}, sessionIds: [], activeSessionId: "", chatOpen: false });
  useAgentDraftRequests.setState({ request: null });
});

test("each session is a workbench document; reopening focuses rather than duplicates", () => {
  openAgentSession(session("a"));
  openAgentSession(session("b"));
  openAgentSession(session("a"));
  expect(tabs().openTabs.map((t) => t.title)).toEqual(["Title a", "Title b"]);
  expect(tabs().openTabs[0].params).toEqual({ sessionId: "a" });
  expect(useWorkbenchTabsStore.getState().activeModuleId).toBe("agenthub");
  expect(activeAgentHubSessionId(useWorkbenchTabsStore.getState())).toBe("a");
});

test("the session on screen follows the focused split group", () => {
  openAgentSession(session("a"));
  openAgentSession(session("b"));
  const state = useWorkbenchTabsStore.getState();
  state.splitTab(tabs().openTabs[1].id);
  expect(activeAgentHubSessionId(useWorkbenchTabsStore.getState())).toBe("b");
  const original = useWorkbenchTabsStore.getState().groups.find((g) => g.activeTabId === tabs().openTabs[0].id)!;
  state.focusGroup(original.id);
  expect(activeAgentHubSessionId(useWorkbenchTabsStore.getState())).toBe("a");
});

test("the new-session tab is not a session tab, and closes on its own", () => {
  openNewAgentSession();
  openAgentSession(session("a"));
  expect(tabs().openTabs).toHaveLength(2);
  expect(agentSessionTabs(useWorkbenchTabsStore.getState()).map((t) => t.params?.sessionId)).toEqual(["a"]);
  expect(useAgentDraftRequests.getState().request).toEqual({ cwd: "", defaultFolder: true });
  closeNewSessionTab();
  expect(tabs().openTabs.map((t) => t.params?.sessionId)).toEqual(["a"]);
});

test("a background tab joins the strip without taking the screen", () => {
  openAgentSession(session("a"));
  useWorkbenchTabsStore.getState().focusModule("notes");
  addAgentSessionTabInBackground(session("b"));
  addAgentSessionTabInBackground(session("b"));
  const state = useWorkbenchTabsStore.getState();
  expect(state.activeModuleId).toBe("notes");
  expect(agentSessionTabs(state).map((t) => t.params?.sessionId)).toEqual(["a", "b"]);
  // Opening it for real later focuses the same tab rather than a second one.
  openAgentSession(session("b"));
  expect(tabs().openTabs).toHaveLength(2);
  expect(activeAgentHubSessionId(useWorkbenchTabsStore.getState())).toBe("b");
});

test("titles follow the session; a rename keeps the tab's id and pin", () => {
  openAgentSession(session("a", null));
  const id = tabs().openTabs[0].id;
  expect(tabs().openTabs[0].title).toBe("Session a");
  useWorkbenchTabsStore.getState().togglePinTab(id);
  syncAgentSessionTabTitles({ a: session("a", "Fix the dialog") });
  expect(tabs().openTabs[0]).toMatchObject({ id, title: "Fix the dialog", pinned: true });
});

test("closing tabs leaves other sessions' tabs alone", () => {
  openAgentSession(session("a"));
  openAgentSession(session("b"));
  openAgentSession(session("c"));
  closeAgentSessionTabs(["a", "c"]);
  expect(tabs().openTabs.map((t) => t.params?.sessionId)).toEqual(["b"]);
});

test("showing a session: its tab inside Agent Hub, the Agent panel anywhere else", () => {
  useAgentSessionsStore.setState({ sessionsById: { a: session("a") }, sessionIds: ["a"] });
  showAgentSession("a");
  expect(useAgentSessionsStore.getState().chatOpen).toBe(true);
  expect(useWorkbenchTabsStore.getState().activeModuleId).toBe("notes");
  expect(agentSessionTabs(useWorkbenchTabsStore.getState()).map((t) => t.params?.sessionId)).toEqual(["a"]);
  expect(agentSessionOnScreen("a")).toBe(true);

  useAgentSessionsStore.setState({ chatOpen: false });
  useWorkbenchTabsStore.getState().focusModule("agenthub");
  showAgentSession("a");
  expect(useAgentSessionsStore.getState().chatOpen).toBe(false);
  expect(activeAgentHubSessionId(useWorkbenchTabsStore.getState())).toBe("a");
  expect(agentSessionOnScreen("a")).toBe(true);
  expect(agentSessionOnScreen("b")).toBe(false);
});
