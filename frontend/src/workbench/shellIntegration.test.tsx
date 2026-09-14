import { beforeEach, describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { useWorkbenchTabsStore } from "../stores/workbenchTabs";
import { useAgentSessionsStore } from "../stores/agentSessions";
import { AgentPermissionToastHost } from "./AgentPermissionToastHost";

// No ui_state writes: the debounced bridge call would reject after this file ends.
useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

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

    it("hides toast when rail is already open and displaying that session", () => {
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

      const html = renderToString(<AgentPermissionToastHost activeModuleId="agenthub" />);
      expect(html).toBe("");
    });
  });
});
