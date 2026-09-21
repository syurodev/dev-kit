import { beforeEach, describe, expect, it } from "bun:test";

import { createElement } from "react";
import { renderToString } from "react-dom/server";
import {
  AgentSessionsSubscriber,
  handleAgentTurnFinished,
  statusAnnouncement,
  statusTransitionAnnouncement,
  turnFinishedAnnouncement,
} from "./AgentSessionsSubscriber";
import { useAgentSessionsStore } from "../stores/agentSessions";
import { useWorkbenchTabsStore } from "../stores/workbenchTabs";
import { sendAgentTurn, type TurnBridge } from "./agentPromptQueue";
import type { InvokeResult } from "../bridge/agenthub";

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

  it("stays running when a late turnFinished lands while the next turn of ours runs", () => {
    useAgentSessionsStore.setState({ turnInFlightBySession: { s1: true } });

    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" });

    expect(tabStatus()).toBe("running");
  });

  it("goes back to idle when our turn's reply comes after its turnFinished", async () => {
    let finish!: (result: InvokeResult) => void;
    const bridge: TurnBridge = {
      invoke: () => new Promise((resolve) => (finish = resolve)),
      getStatus: async (id) => useAgentSessionsStore.getState().sessionsById[id],
    };

    const turn = sendAgentTurn("s1", { id: "p", text: "go", attachments: [], images: [], refs: [] }, bridge);
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, bridge);
    expect(tabStatus()).toBe("running");
    finish({ status: "completed", summary: "", filesChanged: [], turnCount: 1, stopReason: "end_turn" });
    await turn;

    expect(tabStatus()).toBe("idle");
  });
});

describe("what the shell's live region says", () => {
  // Go resets a failed or cancelled turn to idle, and a turn that asked for
  // permission ends awaiting_permission → idle: the status transition says
  // "answer ready" for the wrong turns and nothing for that one.
  it("says how a turn ended, from its outcome", () => {
    expect(turnFinishedAnnouncement("cancelled", [])).toBe("Stopped.");
    expect(turnFinishedAnnouncement("error", [])).toBe("The agent stopped with an error.");
    expect(turnFinishedAnnouncement("completed", [])).toBe("Answer ready.");
  });

  it("adds how long the finished turn worked when its rows carry times", () => {
    const items = [
      { id: "p0", kind: "user_message_chunk", text: "earlier", createdAt: "2026-09-15T09:00:00Z" },
      { id: "p1", kind: "user_message_chunk", text: "go", createdAt: "2026-09-15T10:00:00Z", updatedAt: "2026-09-15T10:00:00Z" },
      { id: "a1", kind: "agent_message_chunk", text: "done", createdAt: "2026-09-15T10:00:05Z", updatedAt: "2026-09-15T10:04:07Z" },
    ];
    expect(turnFinishedAnnouncement("completed", items)).toBe("Answer ready. Worked for 4m 7s");
  });

  it("announces running and waiting, and nothing for the rest", () => {
    expect(statusAnnouncement("running")).toBe("Running…");
    expect(statusAnnouncement("awaiting_permission")).toBe("Waiting for permission.");
    expect(statusAnnouncement("idle")).toBe("");
    expect(statusAnnouncement(undefined)).toBe("");
  });

  it("announces a status only when the same session changes to it after mount", () => {
    // Mounting with nothing loaded yet, then the session list arriving.
    expect(statusTransitionAnnouncement(null, { sessionId: "", status: undefined })).toBe("");
    expect(
      statusTransitionAnnouncement({ sessionId: "s1", status: undefined }, { sessionId: "s1", status: "running" }),
    ).toBe("");
    // Switching to a session that is already running.
    expect(
      statusTransitionAnnouncement({ sessionId: "s1", status: "idle" }, { sessionId: "s2", status: "running" }),
    ).toBe("");
    expect(
      statusTransitionAnnouncement({ sessionId: "s1", status: "idle" }, { sessionId: "s1", status: "running" }),
    ).toBe("Running…");
    expect(
      statusTransitionAnnouncement(
        { sessionId: "s1", status: "running" },
        { sessionId: "s1", status: "awaiting_permission" },
      ),
    ).toBe("Waiting for permission.");
  });

  it("is one polite region, mounted once by the shell", () => {
    const html = renderToString(createElement(AgentSessionsSubscriber));
    expect(html.match(/aria-live=/g)).toHaveLength(1);
    expect(html).toContain('aria-live="polite"');
  });
});
