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

/** A session tab that is not the preview slot: one that stays when the next session is opened. */
function openKept(s: AgentSession) {
  return useWorkbenchTabsStore.getState().openTab({
    moduleId: "agenthub",
    viewId: "agenthub.sessions",
    title: s.title ?? `Session ${s.id}`,
    params: { sessionId: s.id },
    preview: false,
  });
}

beforeEach(() => {
  useWorkbenchTabsStore.setState({ modules: {}, groups: [], focusedGroupId: "", activeModuleId: "notes", recentlyClosed: [] });
  useAgentSessionsStore.setState({ sessionsById: {}, sessionIds: [], activeSessionId: "", chatOpen: false });
  useAgentDraftRequests.setState({ request: null });
});

test("Agent Hub shows one session at a time: the next one replaces it, and reopening does not duplicate", () => {
  openAgentSession(session("a"));
  openAgentSession(session("b"));
  expect(tabs().openTabs.map((t) => t.title)).toEqual(["Title b"]);
  openAgentSession(session("b"));
  expect(tabs().openTabs).toHaveLength(1);
  expect(tabs().openTabs[0].params).toEqual({ sessionId: "b" });
  expect(tabs().openTabs[0].preview).toBe(true);
  expect(useWorkbenchTabsStore.getState().activeModuleId).toBe("agenthub");
  expect(activeAgentHubSessionId(useWorkbenchTabsStore.getState())).toBe("b");
});

test("moving on from a session leaves no tab behind, and the draft and the panel's tabs stay", () => {
  openNewAgentSession();
  addAgentSessionTabInBackground(session("panel"));
  openAgentSession(session("a"));
  openAgentSession(session("b"));
  openAgentSession(session("c"));
  const open = tabs().openTabs.map((t) => (typeof t.params?.sessionId === "string" ? t.params.sessionId : "draft"));
  expect(open.sort()).toEqual(["c", "draft", "panel"]);
  // The panel follows what is open: the session left behind is no longer one of its tabs.
  expect(agentSessionTabs(useWorkbenchTabsStore.getState()).map((t) => t.params?.sessionId).sort()).toEqual(["c", "panel"]);
});

test("a session the Agent panel already holds is focused where it is, not replaced", () => {
  addAgentSessionTabInBackground(session("panel"));
  openAgentSession(session("a"));
  openAgentSession(session("panel"));
  expect(tabs().openTabs.map((t) => t.params?.sessionId).sort()).toEqual(["a", "panel"]);
  expect(activeAgentHubSessionId(useWorkbenchTabsStore.getState())).toBe("panel");
});

test("closing the session on screen offers nothing back: it was only looked at", () => {
  openAgentSession(session("a"));
  closeAgentSessionTabs(["a"]);
  expect(tabs().openTabs).toHaveLength(0);
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(0);
});

test("the session on screen follows the focused split group", () => {
  openKept(session("a"));
  openKept(session("b"));
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

test("a background tab joins the open sessions without taking the screen", () => {
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

test("tabs of deleted sessions are not offered back; a closed tab is", () => {
  openKept(session("a"));
  openKept(session("b"));
  closeAgentSessionTabs(["a"], { remember: false });
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(0);
  closeAgentSessionTabs(["b"]);
  expect(useWorkbenchTabsStore.getState().recentlyClosed.map((e) => e.params?.sessionId)).toEqual(["b"]);
});

test("the new-session tab a session replaced is not offered back", () => {
  openNewAgentSession();
  openAgentSession(session("a"));
  closeNewSessionTab();
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(0);
});

test("a session tab added in the background forgets its closed record", () => {
  openKept(session("a"));
  openKept(session("b"));
  closeAgentSessionTabs(["b"]);
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(1);
  addAgentSessionTabInBackground(session("b"));
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(0);
});

test("a closed session tab is offered back under the session's new title", () => {
  openKept(session("a"));
  openKept(session("b"));
  closeAgentSessionTabs(["b"]);
  syncAgentSessionTabTitles({ b: session("b", "Fix the login") });
  expect(useWorkbenchTabsStore.getState().recentlyClosed.map((e) => e.title)).toEqual(["Fix the login"]);
});

test("closing tabs leaves other sessions' tabs alone", () => {
  openKept(session("a"));
  openKept(session("b"));
  openKept(session("c"));
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
