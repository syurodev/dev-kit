import { beforeEach, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import type { AgentSession } from "@/bridge/agenthub";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { agentSessionTabs, openAgentSession, openNewAgentSession, tabSessionId } from "@/modules/agenthub/documents";
import { AgentPaneTabs, agentPaneSessionId } from "./AgentPaneTabs";
import { agentPanelAllowed } from "./AgentCompanionSlot";

useWorkbenchTabsStore.persist.setOptions({ storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });

function session(id: string, status: AgentSession["status"] = "idle"): AgentSession {
  return { id, agentType: "claude", projectPath: "/repo", status, parentSessionId: null, depth: 0, title: `Title ${id}`, acpSessionId: "" };
}

beforeEach(() => {
  useWorkbenchTabsStore.setState({ modules: {}, groups: [], focusedGroupId: "", activeModuleId: "notes", recentlyClosed: [] });
  const sessions = [session("a"), session("b", "running"), session("c", "closed")];
  useAgentSessionsStore.setState({
    sessionsById: Object.fromEntries(sessions.map((s) => [s.id, s])),
    sessionIds: sessions.map((s) => s.id),
    activeSessionId: "b",
  });
});

test("the panel shows the session it follows only while that session has a tab", () => {
  expect(agentPaneSessionId(["a", "b"], "b")).toBe("b");
  expect(agentPaneSessionId(["a", "b"], "c")).toBe("a");
  expect(agentPaneSessionId([], "c")).toBe("");
});

test("its tabs are Agent Hub's session tabs, in Agent Hub's order, without the new-session tab", () => {
  openAgentSession(session("b"));
  openNewAgentSession();
  openAgentSession(session("a"));
  // What useAgentPaneSessionIds selects (SSR cannot read a store a test set).
  const ids = agentSessionTabs(useWorkbenchTabsStore.getState()).map(tabSessionId);
  expect(ids).toEqual(["b", "a"]);
});

test("with no session tab the header still names the pane", () => {
  expect(renderToString(<AgentPaneTabs />)).toContain(">Agent<");
});

test("the Agent panel is never shown inside Agent Hub", () => {
  expect(agentPanelAllowed("agenthub")).toBe(false);
  expect(agentPanelAllowed("notes")).toBe(true);
  expect(agentPanelAllowed(null)).toBe(true);
});
