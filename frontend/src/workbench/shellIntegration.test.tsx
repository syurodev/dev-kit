import { beforeEach, describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { useWorkbenchTabsStore } from "../stores/workbenchTabs";
import { useAgentSessionsStore } from "../stores/agentSessions";
import { AgentPermissionToastHost } from "./AgentPermissionToastHost";
import type { AgentSession } from "../bridge/agenthub";

// No ui_state writes: the debounced bridge call would reject after this file ends.
useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

/** Agent Hub on screen with a session's tab focused — what "the hub shows it" means now. */
function showHubSession(sessionId: string) {
  const id = `agenthub:agenthub.sessions:sessionId=${sessionId}`;
  useWorkbenchTabsStore.setState((state) => ({
    activeModuleId: "agenthub",
    groups: [],
    modules: {
      ...state.modules,
      agenthub: {
        openTabs: [
          { id, moduleId: "agenthub", viewId: "agenthub.sessions", title: sessionId, params: { sessionId } },
        ],
        activeTabId: id,
        tabMru: [id],
      },
    },
  }));
}

describe("Phase 4 Shell Integration", () => {
  beforeEach(() => {
    useWorkbenchTabsStore.setState({
      activeModuleId: "agenthub",
      modules: {
        agenthub: {
          openTabs: [
            {
              id: "agenthub:agenthub.sessions",
              moduleId: "agenthub",
              viewId: "agenthub.sessions",
              title: "Agent Hub",
              status: "idle",
            },
          ],
          activeTabId: "agenthub:agenthub.sessions",
          tabMru: ["agenthub:agenthub.sessions"],
        },
        notes: {
          openTabs: [
            {
              id: "notes:notes.list",
              moduleId: "notes",
              viewId: "notes.list",
              title: "Notes",
              status: "idle",
            },
          ],
          activeTabId: "notes:notes.list",
          tabMru: ["notes:notes.list"],
        },
      },
    });

    useAgentSessionsStore.setState({
      sessionsById: {
        "sess-1": {
          id: "sess-1",
          agentType: "claude",
          projectPath: "/test/project",
          status: "idle",
          parentSessionId: null,
          depth: 0,
          title: "Root Agent",
          acpSessionId: "acp-1",
        },
      },
      sessionIds: ["sess-1"],
      activeSessionId: "sess-1",
      chatOpen: false,
      pending: null,
      pendingQueue: [],
      elicitation: null,
      elicitationQueue: [],
    });
  });

  describe("Tab Status Synchronization", () => {
    it("setTabStatus updates the matching agenthub tab status", () => {
      const { setTabStatus } = useWorkbenchTabsStore.getState();
      setTabStatus("agenthub:agenthub.sessions", "running");

      const { modules } = useWorkbenchTabsStore.getState();
      const tab = modules.agenthub.openTabs.find((t) => t.id === "agenthub:agenthub.sessions");
      expect(tab?.status).toBe("running");

      // Another module's partition is untouched — not even a new object.
      const notesTab = modules.notes.openTabs.find((t) => t.id === "notes:notes.list");
      expect(notesTab?.status).toBe("idle");
    });
  });

  describe("AgentPermissionToastHost", () => {
    it("renders nothing when no permission is pending", () => {
      const html = renderToString(<AgentPermissionToastHost activeModuleId="notes" />);
      expect(html).toBe("");
    });

    it("renders permission toast when pending and rail is closed", () => {
      useAgentSessionsStore.setState({
        chatOpen: false,
        pending: {
          id: "perm-host-1",
          sessionId: "sess-1",
          caller: "agent:claude",
          createdAt: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 60_000).toISOString(),
          title: "Root Agent",
          depth: 0,
          params: {
            command: "rm -rf build",
            options: [{ optionId: "allow_once", name: "Allow once" }],
          },
        },
      });

      const html = renderToString(<AgentPermissionToastHost activeModuleId="notes" />);
      expect(html).toContain('data-testid="agent-permission-toast"');
      expect(html).toContain("rm -rf build");
      expect(html).toContain("Root Agent");
      expect(html).toContain("Open rail");
      expect(html).not.toContain("Allow always");
    });

    it("floats when the module shows a different session than the one asking", () => {
      // The gap this closes: inside Agent Hub, looking at session A, a request
      // from B was asked nowhere at all — no inline card (wrong session), no
      // toast (right module) — and the gate denies it after 60s.
      useAgentSessionsStore.setState({
        chatOpen: false,
        activeSessionId: "sess-1",
        pending: {
          id: "perm-other",
          sessionId: "sess-2",
          caller: "agent:claude",
          createdAt: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 60_000).toISOString(),
          title: "Other Agent",
          depth: 1,
          params: {},
        },
      });

      const html = renderToString(<AgentPermissionToastHost activeModuleId="agenthub" />);
      expect(html).toContain('data-testid="agent-permission-toast"');
      expect(html).toContain("Other Agent");
    });

    it("stays away when the module is showing the session that is asking", () => {
      showHubSession("sess-1");
      useAgentSessionsStore.setState({
        chatOpen: false,
        activeSessionId: "sess-1",
        pending: {
          id: "perm-inline",
          sessionId: "sess-1",
          caller: "agent:claude",
          createdAt: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 60_000).toISOString(),
          title: "Root Agent",
          depth: 0,
          params: {},
        },
      });

      // AgentHubContent asks it above the composer instead.
      expect(renderToString(<AgentPermissionToastHost activeModuleId="agenthub" />)).toBe("");
    });

    it("uses a corner pill for another request while the visible session has its own inline request", () => {
      showHubSession("sess-1");
      const item = (id: string, sessionId: string, createdAt: string) => ({
        id, sessionId, caller: "agent:claude", createdAt, expiresAt: new Date(Date.now() + 60_000).toISOString(),
        title: id, depth: 0, params: {},
      });
      useAgentSessionsStore.setState({
        pending: item("inline", "sess-1", "2026-10-25T10:00:00Z"),
        pendingQueue: [
          item("inline", "sess-1", "2026-10-25T10:00:00Z"),
          item("newer", "sess-3", "2026-10-25T10:02:00Z"),
          item("older", "sess-2", "2026-10-25T10:01:00Z"),
        ],
      });
      const html = renderToString(
        <AgentPermissionToastHost activeModuleId="agenthub" session={{ id: "sess-2", title: "older" } as AgentSession} />,
      );
      expect(html).toContain('data-testid="agent-permission-waiting-pill"');
      expect(html).toContain("1 request waiting in <!-- -->older");
      expect(html).not.toContain('data-testid="agent-permission-toast"');
      expect(html).toContain("top-4");
    });

    it("sits above the z-50 modal tier, marked so a dialog does not dismiss on its clicks", () => {
      const pending = (id: string) => ({
        id,
        sessionId: "sess-1",
        caller: "agent:claude",
        createdAt: new Date().toISOString(),
        expiresAt: new Date(Date.now() + 60_000).toISOString(),
        title: "Root Agent",
        depth: 0,
        params: {},
      });
      useAgentSessionsStore.setState({
        chatOpen: false,
        pending: pending("perm-a"),
        pendingQueue: [pending("perm-a"), pending("perm-b"), pending("perm-c")],
      });

      const html = renderToString(<AgentPermissionToastHost activeModuleId="notes" />);
      expect(html).toContain("z-60");
      expect(html).toContain("data-shell-float");
      // The queue holds the request on screen too; the pill counts the other two.
      expect(html).toContain('data-testid="toast-queue"');
      expect(html).toContain("+2 waiting");
    });

    const question = (id: string, sessionId: string) => ({
      id,
      sessionId,
      toolCallId: "",
      message: "Which region?",
      params: {
        message: "Which region?",
        requestedSchema: { type: "object", properties: { region: { type: "string", enum: ["us", "eu"] } } },
      },
      caller: `agent:${sessionId}`,
      title: "Question Agent",
      depth: 0,
      createdAt: new Date().toISOString(),
    });

    it("floats a question from a session nobody is looking at, without its form", () => {
      useAgentSessionsStore.setState({ chatOpen: false, elicitationQueue: [question("q-1", "sess-1")] });

      const html = renderToString(<AgentPermissionToastHost activeModuleId="notes" />);
      expect(html).toContain('data-testid="agent-question-toast"');
      expect(html).toContain("Which region?");
      expect(html).toContain("1 question");
      expect(html).not.toContain("questionnaire");
    });

    it("gives the one corner to the permission when a question is waiting too", () => {
      useAgentSessionsStore.setState({
        chatOpen: false,
        pending: {
          id: "perm-both",
          sessionId: "sess-1",
          caller: "agent:claude",
          createdAt: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 60_000).toISOString(),
          title: "Root Agent",
          depth: 0,
          params: {},
        },
        elicitationQueue: [question("q-both", "sess-1")],
      });

      const html = renderToString(<AgentPermissionToastHost activeModuleId="notes" />);
      expect(html).toContain('data-testid="agent-permission-toast"');
      expect(html).not.toContain('data-testid="agent-question-toast"');
    });

    it("leaves a question to the inline card when its session is on screen, but floats another session's", () => {
      showHubSession("sess-1");
      useAgentSessionsStore.setState({ activeSessionId: "sess-1", elicitationQueue: [question("q-here", "sess-1")] });
      expect(renderToString(<AgentPermissionToastHost activeModuleId="agenthub" />)).toBe("");

      useAgentSessionsStore.setState({
        elicitationQueue: [question("q-here", "sess-1"), question("q-there", "sess-2")],
      });
      const html = renderToString(<AgentPermissionToastHost activeModuleId="agenthub" />);
      expect(html).toContain('data-testid="agent-question-toast"');
    });

    it("floats over Agent Hub when its tab on screen is another session, whatever the panel follows", () => {
      showHubSession("sess-2");
      useAgentSessionsStore.setState({
        chatOpen: true,
        activeSessionId: "sess-1",
        pending: {
          id: "perm-hub-other",
          sessionId: "sess-1",
          caller: "agent:claude",
          createdAt: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 60_000).toISOString(),
          title: "Root Agent",
          depth: 0,
          params: {},
        },
      });

      // The Agent panel is not shown in Agent Hub, so it cannot be asking.
      const html = renderToString(<AgentPermissionToastHost activeModuleId="agenthub" />);
      expect(html).toContain('data-testid="agent-permission-toast"');
    });

    it("hides toast when rail is already open and displaying that session", () => {
      // The panel shows a session that has a tab (one of Agent Hub's).
      showHubSession("sess-1");
      useAgentSessionsStore.setState({
        chatOpen: true,
        activeSessionId: "sess-1",
        pending: {
          id: "perm-host-1",
          sessionId: "sess-1",
          caller: "agent:claude",
          createdAt: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 60_000).toISOString(),
          title: "Root Agent",
          depth: 0,
          params: {},
        },
      });

      const html = renderToString(<AgentPermissionToastHost activeModuleId="notes" />);
      expect(html).toBe("");
    });
  });
});
