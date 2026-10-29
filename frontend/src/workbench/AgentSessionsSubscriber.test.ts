import { beforeEach, describe, expect, it } from "bun:test";

import { createElement } from "react";
import { renderToString } from "react-dom/server";
import {
  adoptCreatedSession,
  AgentSessionsSubscriber,
  handleAgentActivity,
  handleAgentTurnFinished,
  handleAgentTurnStarted,
  handleSessionUpdateTabs,
  isPeerMessage,
  mirrorPromptQueueInOrder,
  promptQueueChanges,
  promptQueueSnapshot,
  statusAnnouncement,
  statusTransitionAnnouncement,
  turnFinishedAnnouncement,
} from "./AgentSessionsSubscriber";
import { useAgentSessionsStore } from "../stores/agentSessions";
import { useWorkbenchTabsStore } from "../stores/workbenchTabs";
import { sendAgentTurn, setAgentHubTabsStatus, type TurnBridge } from "./agentPromptQueue";
import type { AgentSession, InvokeResult } from "../bridge/agenthub";
import { queryClient } from "../lib/query";
import { agentEnvironmentKey } from "../modules/agenthub/environment/environmentKey";

// No ui_state writes: the debounced bridge call would reject after this file ends.
useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

const tabStatus = () => useWorkbenchTabsStore.getState().modules.agenthub.openTabs[0].status;
/** The new-session tab: no session of its own, so no session's status. */
const homeStatus = () => useWorkbenchTabsStore.getState().modules.agenthub.openTabs[1].status;

describe("an Agent Hub tab when a turn finishes", () => {
  beforeEach(() => {
    useWorkbenchTabsStore.setState({
      // Elsewhere, so the finished turn is unseen.
      activeModuleId: "notes",
      modules: {
        agenthub: {
          openTabs: [
            {
              id: "agenthub:agenthub.sessions:sessionId=s1",
              moduleId: "agenthub",
              viewId: "agenthub.sessions",
              title: "s1",
              params: { sessionId: "s1" },
              status: "running",
            },
            { id: "agenthub:agenthub.sessions", moduleId: "agenthub", viewId: "agenthub.sessions", title: "New session", status: "idle" },
          ],
          activeTabId: "agenthub:agenthub.sessions:sessionId=s1",
          tabMru: ["agenthub:agenthub.sessions:sessionId=s1"],
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

  it("clears the rail's provider-retry line when the turn ends", () => {
    useAgentSessionsStore.getState().setProviderRetry("s1", { attempt: 11, message: "Endpoint is unavailable.", status: 503 });
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "error" });
    expect(useAgentSessionsStore.getState().providerRetries.s1).toBeUndefined();
  });

  it("marks a completed turn finished, beside the unread badge", () => {
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" });

    // The strip shows it until the tab is looked at (tabs/seenSuccess.ts).
    expect(tabStatus()).toBe("success");
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

  it("keeps running through a tool call that failed: only the turn's end says it failed", () => {
    // What the update carries is not read: any update of a turn means running.
    handleSessionUpdateTabs({ sessionId: "s1" });

    expect(tabStatus()).toBe("running");
  });

  it("leaves the new-session tab alone: it shows no session", () => {
    handleAgentTurnFinished({ sessionId: "s1", status: "error", outcome: "error" });
    setAgentHubTabsStatus("s1", "running");

    expect(tabStatus()).toBe("running");
    expect(homeStatus()).toBe("idle");
  });

  it("stays running when a late turnFinished lands while the next turn of ours runs", () => {
    useAgentSessionsStore.setState({ turnInFlightBySession: { s1: true } });

    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" });

    expect(tabStatus()).toBe("running");
  });

  it("reads the preview of a turn it did not send, without moving the status back", async () => {
    const bridge: TurnBridge = {
      invoke: () => new Promise(() => {}),
      // A row read just before the next queued turn reached the backend.
      getStatus: async (id) => ({ ...useAgentSessionsStore.getState().sessionsById[id], status: "idle", preview: "Fixed the flaky test." }),
    };

    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, bridge);
    useAgentSessionsStore.getState().upsertSession({ ...useAgentSessionsStore.getState().sessionsById.s1, status: "running" });
    await new Promise((resolve) => setTimeout(resolve, 0));

    const row = useAgentSessionsStore.getState().sessionsById.s1;
    expect(row.preview).toBe("Fixed the flaky test.");
    expect(row.status).toBe("running");
  });

  it("takes the title Go gave a turn it did not send, but never replaces one the row has", async () => {
    const named = (title: string): TurnBridge => ({
      invoke: () => new Promise(() => {}),
      getStatus: async (id) => ({ ...useAgentSessionsStore.getState().sessionsById[id], title }),
    });
    const row = () => useAgentSessionsStore.getState().sessionsById.s1;

    useAgentSessionsStore.getState().upsertSession({ ...row(), title: null });
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, named("Fix the flaky test"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(row().title).toBe("Fix the flaky test");

    useAgentSessionsStore.getState().upsertSession({ ...row(), title: "My name" });
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, named("Something else"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(row().title).toBe("My name");
  });

  it("keeps the newest preview when two reads answer out of order", async () => {
    const replies: Array<(preview: string, updatedAt: number) => void> = [];
    const bridge: TurnBridge = {
      invoke: () => new Promise(() => {}),
      getStatus: (id) =>
        new Promise((resolve) => {
          replies.push((preview, updatedAt) =>
            resolve({ ...useAgentSessionsStore.getState().sessionsById[id], preview, updatedAt }),
          );
        }),
    };

    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, bridge);
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, bridge);
    expect(replies.length).toBe(2);
    replies[1]("Second answer.", 2000);
    await new Promise((resolve) => setTimeout(resolve, 0));
    replies[0]("First answer.", 1000);
    await new Promise((resolve) => setTimeout(resolve, 0));

    const row = useAgentSessionsStore.getState().sessionsById.s1;
    expect(row.preview).toBe("Second answer.");
    expect(row.updatedAt).toBe(2000);
  });

  it("marks the finished session's Environment stale, and no other session's", () => {
    queryClient.setQueryData(agentEnvironmentKey("claude", "s1"), { entries: [] });
    queryClient.setQueryData(agentEnvironmentKey("claude", "s2"), { entries: [] });
    queryClient.setQueryData(agentEnvironmentKey("claude", ""), { entries: [] });
    const stale = (key: readonly string[]) => queryClient.getQueryState(key)?.isInvalidated;

    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" });

    expect(stale(agentEnvironmentKey("claude", "s1"))).toBe(true);
    expect(stale(agentEnvironmentKey("claude", "s2"))).toBe(false);
    expect(stale(agentEnvironmentKey("claude", ""))).toBe(false);
  });

  it("marks it stale for a turn of its own too", () => {
    queryClient.setQueryData(agentEnvironmentKey("claude", "s1"), { entries: [] });
    useAgentSessionsStore.setState({ turnInFlightBySession: { s1: true } });

    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" });

    expect(queryClient.getQueryState(agentEnvironmentKey("claude", "s1"))?.isInvalidated).toBe(true);
  });

  it("does not read the row for a turn of its own", async () => {
    let reads = 0;
    const bridge: TurnBridge = {
      invoke: () => new Promise(() => {}),
      getStatus: async (id) => {
        reads += 1;
        return useAgentSessionsStore.getState().sessionsById[id];
      },
    };
    useAgentSessionsStore.setState({ turnInFlightBySession: { s1: true } });

    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, bridge);
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(reads).toBe(0);
  });

  it("marks our turn finished when its reply comes after its turnFinished", async () => {
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

    expect(tabStatus()).toBe("success");
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

describe("messages from other sessions and the person's own queue", () => {
  const queued = (n: number) => Array.from({ length: n }, (_, i) => ({ id: `q${i}`, text: "t", attachments: [], images: [], refs: [] }));

  it("recognises a user message the person did not type", () => {
    expect(isPeerMessage({ parsed: { kind: "user_message_chunk", source: { kind: "session", sessionId: "a", hop: 1 } } as never })).toBe(true);
    expect(isPeerMessage({ parsed: { kind: "user_message_chunk" } as never })).toBe(false);
    expect(isPeerMessage({ parsed: { kind: "agent_message_chunk", source: { kind: "session", hop: 1 } } as never })).toBe(false);
    expect(isPeerMessage({ parsed: null })).toBe(false);
  });

  it("reports only the sessions whose queue or pause changed", () => {
    const none = { queuedPromptsBySession: {}, queuePausedBySession: {} };
    const one = { queuedPromptsBySession: { s1: queued(2) }, queuePausedBySession: { s1: true, s2: false } };
    expect(promptQueueChanges(one, none)).toEqual([{ sessionId: "s1", waiting: 2, paused: true }]);
    // Nothing moved: nothing is sent.
    expect(promptQueueChanges(one, one)).toEqual([]);
    // Drained and resumed: Go is told the session is free again.
    expect(promptQueueChanges(none, one)).toEqual([{ sessionId: "s1", waiting: 0, paused: false }]);
    // One more prompt waiting is a change.
    expect(promptQueueChanges({ ...one, queuedPromptsBySession: { s1: queued(3) } }, one)).toEqual([{ sessionId: "s1", waiting: 3, paused: true }]);
  });

  it("mirrors every startup session, including empty queues, so recovered peer work can resume", () => {
    const state = {
      queuedPromptsBySession: { paused: queued(2) },
      queuePausedBySession: { paused: true },
    };
    expect(promptQueueSnapshot(["empty", "paused"], state)).toEqual([
      { sessionId: "empty", waiting: 0, paused: false },
      { sessionId: "paused", waiting: 2, paused: true },
    ]);
  });

  it("does not let a late startup mirror overwrite a newer queue pause", async () => {
    const calls: [string, number, boolean][] = [];
    let release!: () => void;
    let started!: () => void;
    const blocked = new Promise<void>((resolve) => { release = resolve; });
    const entered = new Promise<void>((resolve) => { started = resolve; });
    const send = async (sessionId: string, waiting: number, paused: boolean) => {
      calls.push([sessionId, waiting, paused]);
      if (calls.length === 1) {
        started();
        await blocked;
      }
    };

    const initial = mirrorPromptQueueInOrder("s1", 0, false, send);
    await entered;
    const newer = mirrorPromptQueueInOrder("s1", 2, true, send);
    expect(calls).toEqual([["s1", 0, false]]);
    release();
    await Promise.all([initial, newer]);
    expect(calls).toEqual([["s1", 0, false], ["s1", 2, true]]);
  });

  it("counts them on the session until it is looked at, and never as needing the person", () => {
    const store = useAgentSessionsStore;
    store.setState({ peerUnreadBySession: {}, unreadCompletedBySession: {} });
    store.getState().notePeerMessage("s1");
    store.getState().notePeerMessage("s1");
    store.getState().notePeerMessage("s2");
    expect(store.getState().peerUnreadBySession).toEqual({ s1: 2, s2: 1 });
    // Looking at s1 clears its count and leaves s2's.
    store.getState().markCompletedRead("s1");
    expect(store.getState().peerUnreadBySession).toEqual({ s2: 1 });
    // Deleting a session takes its count with it.
    store.getState().removeSession("s2");
    expect(store.getState().peerUnreadBySession).toEqual({});
  });
});

describe("a session another caller created", () => {
  const child = { id: "child-1", agentType: "opencode", status: "running", parentSessionId: "p" } as unknown as AgentSession;

  beforeEach(() => {
    useAgentSessionsStore.setState({ sessionsById: {}, sessionsLoaded: true });
  });

  it("is added to the list this window loaded before it existed", async () => {
    await adoptCreatedSession("child-1", async () => child);
    expect(useAgentSessionsStore.getState().sessionsById["child-1"]?.parentSessionId).toBe("p");
  });

  it("is not read again when the window already has it", async () => {
    useAgentSessionsStore.setState({ sessionsById: { "child-1": { ...child, status: "idle" } as AgentSession } });
    let reads = 0;
    await adoptCreatedSession("child-1", async () => {
      reads += 1;
      return child;
    });
    expect(reads).toBe(0);
    expect(useAgentSessionsStore.getState().sessionsById["child-1"]?.status).toBe("idle");
  });

  it("is left out quietly when the row cannot be read", async () => {
    await adoptCreatedSession("child-1", async () => {
      throw new Error("gone");
    });
    expect(useAgentSessionsStore.getState().sessionsById["child-1"]).toBeUndefined();
  });
});

describe("a turn another caller started", () => {
  const row = () => useAgentSessionsStore.getState().sessionsById.s1;

  beforeEach(() => {
    useAgentSessionsStore.setState({
      sessionsById: { s1: { id: "s1", agentType: "opencode", status: "idle" } as never },
      turnInFlightBySession: {},
    });
  });

  it("shows the session as running in the rail", () => {
    handleAgentTurnStarted("s1");
    expect(row().status).toBe("running");
  });

  it("leaves a turn this window sent to the call that sent it", () => {
    useAgentSessionsStore.setState({ turnInFlightBySession: { s1: true } });
    handleAgentTurnStarted("s1");
    expect(row().status).toBe("idle");
  });

  it("does not turn a waiting session back into a running one, nor invent a session", () => {
    useAgentSessionsStore.getState().upsertSession({ ...row(), status: "awaiting_permission" });
    handleAgentTurnStarted("s1");
    expect(row().status).toBe("awaiting_permission");
    handleAgentTurnStarted("nobody");
    expect(useAgentSessionsStore.getState().sessionsById.nobody).toBeUndefined();
  });
});

describe("a session's own work (agenthub:activity)", () => {
  const row = () => useAgentSessionsStore.getState().sessionsById.s1;
  const ON = { agentTurn: true, tasks: 0 };
  const OFF = { agentTurn: false, tasks: 0 };

  beforeEach(() => {
    useWorkbenchTabsStore.setState({
      activeModuleId: "notes",
      modules: {
        agenthub: {
          openTabs: [
            { id: "agenthub:agenthub.sessions:sessionId=s1", moduleId: "agenthub", viewId: "agenthub.sessions", title: "s1", params: { sessionId: "s1" }, status: "idle" },
            { id: "agenthub:agenthub.sessions", moduleId: "agenthub", viewId: "agenthub.sessions", title: "New session", status: "idle" },
          ],
          activeTabId: "agenthub:agenthub.sessions:sessionId=s1",
          tabMru: ["agenthub:agenthub.sessions:sessionId=s1"],
        },
      },
    });
    useAgentSessionsStore.setState({
      sessionsById: {
        s1: { id: "s1", agentType: "claude", projectPath: "/p", status: "idle", parentSessionId: null, depth: 0, title: "s1", acpSessionId: "", preview: "Build chạy nền." },
      },
      sessionIds: ["s1"],
      activeSessionId: "s1",
      unreadCompletedBySession: {},
      turnInFlightBySession: {},
    });
  });

  const fresh = (preview: string): TurnBridge => ({
    invoke: () => new Promise(() => {}),
    getStatus: async (id) => ({ ...useAgentSessionsStore.getState().sessionsById[id], preview, updatedAt: 5000 }),
  });
  const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

  it("keeps the activity on the row and the stored status where it was", () => {
    handleAgentActivity({ sessionId: "s1", activity: ON }, fresh("x"));
    expect(row().activity).toEqual(ON);
    expect(row().status).toBe("idle");
  });

  it("shows the tab running while the agent's turn runs", () => {
    handleAgentActivity({ sessionId: "s1", activity: ON }, fresh("x"));
    expect(tabStatus()).toBe("running");
    expect(homeStatus()).toBe("idle");
  });

  it("reads the row again when the agent's turn ends: the preview moved with it", async () => {
    handleAgentActivity({ sessionId: "s1", activity: ON }, fresh("x"));
    handleAgentActivity({ sessionId: "s1", activity: OFF }, fresh("Build xanh rồi."));
    await settle();
    expect(row().preview).toBe("Build xanh rồi.");
    expect(row().activity).toBeUndefined();
    expect(row().status).toBe("idle");
  });

  it("marks the tab finished and the result unread when the agent's turn ends out of sight", () => {
    handleAgentActivity({ sessionId: "s1", activity: ON }, fresh("x"));
    handleAgentActivity({ sessionId: "s1", activity: OFF }, fresh("y"));
    expect(tabStatus()).toBe("success");
    expect(useAgentSessionsStore.getState().unreadCompletedBySession.s1).toBe(true);
  });

  it("does not read the row again for activity that is not an agent turn ending", async () => {
    let reads = 0;
    const bridge: TurnBridge = { invoke: () => new Promise(() => {}), getStatus: async (id) => { reads += 1; return row() ?? ({ id } as never); } };
    handleAgentActivity({ sessionId: "s1", activity: { agentTurn: false, tasks: 1 } }, bridge);
    handleAgentActivity({ sessionId: "s1", activity: OFF }, bridge);
    await settle();
    expect(reads).toBe(0);
    expect(tabStatus()).toBe("idle");
  });

  it("leaves the tab to a turn of ours that is running when the agent's turn ends", () => {
    handleAgentActivity({ sessionId: "s1", activity: ON }, fresh("x"));
    useAgentSessionsStore.setState({ turnInFlightBySession: { s1: true } });
    useAgentSessionsStore.getState().upsertSession({ ...row(), status: "running" });
    setAgentHubTabsStatus("s1", "running");
    handleAgentActivity({ sessionId: "s1", activity: OFF }, fresh("y"));
    expect(tabStatus()).toBe("running");
  });

  it("ignores a session it has not loaded", async () => {
    handleAgentActivity({ sessionId: "nobody", activity: ON }, fresh("x"));
    await settle();
    expect(useAgentSessionsStore.getState().sessionsById.nobody).toBeUndefined();
  });

  it("does not let an update of background work paint an idle session's tab running", () => {
    // A shell task's progress or a late message arrives while nothing is working.
    handleSessionUpdateTabs({ sessionId: "s1" });
    expect(tabStatus()).toBe("idle");
    handleAgentActivity({ sessionId: "s1", activity: ON }, fresh("x"));
    handleSessionUpdateTabs({ sessionId: "s1" });
    expect(tabStatus()).toBe("running");
  });

  it("still marks a session it does not know running: its turn may be the first thing it hears of", () => {
    handleSessionUpdateTabs({ sessionId: "nobody" });
    expect(tabStatus()).toBe("idle"); // no tab of its own either; nothing to paint, nothing thrown
  });
});
