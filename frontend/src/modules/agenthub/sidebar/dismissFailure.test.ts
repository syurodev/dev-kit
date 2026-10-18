import { afterEach, beforeEach, describe, expect, it } from "bun:test";
import type { AgentSession } from "@/bridge/agenthub";
import { useAgentPinsStore } from "@/stores/agentPins";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { handleAgentTurnFinished, pruneDismissedFailures, watchDismissedFailures } from "@/workbench/AgentSessionsSubscriber";
import { setAgentHubTabsStatus } from "@/workbench/agentPromptQueue";
import { dismissSessionFailure } from "./dismissFailure";

// No ui_state writes: the debounced bridge call would reject after this file ends.
const noStorage = { getItem: () => null, setItem: () => {}, removeItem: () => {} };
useWorkbenchTabsStore.persist.setOptions({ storage: noStorage });
useAgentPinsStore.persist.setOptions({ storage: noStorage });

const row = (id: string, over: Partial<AgentSession> = {}): AgentSession => ({
  id,
  agentType: "opencode",
  projectPath: "/p",
  workspaceId: "",
  status: "error",
  parentSessionId: null,
  depth: 0,
  title: id,
  acpSessionId: "",
  updatedAt: 10,
  ...over,
});

const tabStatus = () => useWorkbenchTabsStore.getState().modules.agenthub.openTabs[0].status;
const dismissed = () => useAgentPinsStore.getState().dismissedFailures;

function load(...sessions: AgentSession[]) {
  useAgentSessionsStore.setState({
    sessionsById: Object.fromEntries(sessions.map((s) => [s.id, s])),
    sessionIds: sessions.map((s) => s.id),
    sessionsLoaded: true,
    unreadCompletedBySession: Object.fromEntries(sessions.map((s) => [s.id, true])),
    peerUnreadBySession: { s1: 2 },
    pendingQueue: [],
    elicitationQueue: [],
    turnInFlightBySession: {},
  });
}

describe("dismissing a failed session", () => {
  let stopWatching = () => {};
  afterEach(() => stopWatching());
  beforeEach(() => {
    useAgentPinsStore.setState({ dismissedFailures: {} });
    useWorkbenchTabsStore.setState({
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
              status: "error",
            },
          ],
          activeTabId: "agenthub:agenthub.sessions:sessionId=s1",
          tabMru: ["agenthub:agenthub.sessions:sessionId=s1"],
        },
      },
    });
    load(row("s1"));
    stopWatching = watchDismissedFailures();
  });

  it("remembers the row's time, settles the tab and the unread mark, and touches nothing else", () => {
    expect(dismissSessionFailure({ id: "s1" })).toBe(true);
    expect(dismissed()).toEqual({ s1: 10 });
    expect(tabStatus()).toBe("idle");
    const sessions = useAgentSessionsStore.getState();
    expect(sessions.unreadCompletedBySession.s1).toBeUndefined();
    // Messages from other sessions are a different thing the person has not seen.
    expect(sessions.peerUnreadBySession.s1).toBe(2);
    // The session is neither closed nor changed.
    expect(sessions.sessionsById.s1.status).toBe("error");
  });

  it("refuses a session that is not failed, is gone, or already dismissed", () => {
    load(row("s1", { status: "idle" }));
    expect(dismissSessionFailure({ id: "s1" })).toBe(false);
    expect(dismissSessionFailure({ id: "nope" })).toBe(false);
    load(row("s1"));
    expect(dismissSessionFailure({ id: "s1" })).toBe(true);
    expect(dismissSessionFailure({ id: "s1" })).toBe(false);
    expect(dismissed()).toEqual({ s1: 10 });
  });

  it("never dismisses a session that waits on a permission or a question", () => {
    useAgentSessionsStore.setState({ elicitationQueue: [{ sessionId: "s1" } as never] });
    expect(dismissSessionFailure({ id: "s1" })).toBe(false);
    expect(dismissed()).toEqual({});
    expect(tabStatus()).toBe("error");
  });

  it("a dismissed session's tab stays settled when something re-reports the same failure", () => {
    dismissSessionFailure({ id: "s1" });
    setAgentHubTabsStatus("s1", "error");
    expect(tabStatus()).toBe("idle");
  });

  it("comes back by failing again: recovery forgets the dismissal, and the next failure is news", () => {
    dismissSessionFailure({ id: "s1" });
    // The person resumed it: the row runs, then fails again.
    useAgentSessionsStore.getState().upsertSession(row("s1", { status: "running", updatedAt: 11 }));
    expect(dismissed()).toEqual({});
    useAgentSessionsStore.getState().upsertSession(row("s1", { status: "error", updatedAt: 12 }));
    handleAgentTurnFinished({ sessionId: "s1", status: "error", outcome: "error" });
    expect(tabStatus()).toBe("error");
  });

  it("is pruned for sessions that are gone, but never before the list has loaded", () => {
    stopWatching();
    useAgentPinsStore.setState({ dismissedFailures: { s1: 10, gone: 5 } });
    useAgentSessionsStore.setState({ sessionsLoaded: false, sessionsById: {}, sessionIds: [] });
    pruneDismissedFailures();
    expect(dismissed()).toEqual({ s1: 10, gone: 5 });
    load(row("s1"));
    pruneDismissedFailures();
    expect(dismissed()).toEqual({ s1: 10 });
  });
});
