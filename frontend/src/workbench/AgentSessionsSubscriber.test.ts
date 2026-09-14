import { beforeEach, describe, expect, it } from "bun:test";

import { handleAgentTurnFinished } from "./AgentSessionsSubscriber";
import { useAgentSessionsStore } from "../stores/agentSessions";
import { useWorkbenchTabsStore } from "../stores/workbenchTabs";

// No ui_state writes: the debounced bridge call would reject after this file ends.
useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

const tabStatus = () => useWorkbenchTabsStore.getState().modules.agenthub.openTabs[0].status;

describe("an Agent Hub tab when a turn finishes", () => {
  beforeEach(() => {
    useWorkbenchTabsStore.setState({
      // Elsewhere, so the finished turn is unseen.
      activeModuleId: "notes",
      modules: {
        agenthub: {
          openTabs: [
            { id: "agenthub:agenthub.sessions", moduleId: "agenthub", viewId: "agenthub.sessions", title: "Agent Hub", status: "running" },
          ],
          activeTabId: "agenthub:agenthub.sessions",
          tabMru: ["agenthub:agenthub.sessions"],
        },
      },
    });
    useAgentSessionsStore.setState({
      sessionsById: {
        s1: { id: "s1", agentType: "claude", projectPath: "/p", status: "running", parentSessionId: null, depth: 0, title: "s1", acpSessionId: "" },
      },
      sessionIds: ["s1"],
      activeSessionId: "s1",
      chatOpen: false,
      unreadCompletedBySession: {},
      queuedPromptsBySession: {},
      turnInFlightBySession: {},
      queuePausedBySession: {},
      pendingQueue: [],
    });
  });

  it("goes back to idle after a completed turn, leaving 'unseen' to the unread badge", () => {
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" });

    expect(tabStatus()).toBe("idle");
    expect(useAgentSessionsStore.getState().unreadCompletedBySession.s1).toBe(true);
  });

  it("goes back to idle after a cancelled turn", () => {
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "cancelled" });

    expect(tabStatus()).toBe("idle");
  });

  it("shows the error of a turn that failed", () => {
    handleAgentTurnFinished({ sessionId: "s1", status: "error", outcome: "error" });

    expect(tabStatus()).toBe("error");
  });
});
