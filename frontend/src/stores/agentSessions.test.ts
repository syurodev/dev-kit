import { beforeEach, describe, expect, it } from "bun:test";

import { useAgentSessionsStore } from "./agentSessions";
import type { AgentSession } from "../bridge/agenthub";

const session = (id: string): AgentSession => ({
  id,
  agentType: "claude",
  projectPath: "/p",
  status: "running",
  parentSessionId: null,
  depth: 0,
  title: id,
  acpSessionId: `acp-${id}`,
});

describe("subagents in the session store", () => {
  beforeEach(() => {
    useAgentSessionsStore.setState({
      sessionsById: {},
      sessionIds: [],
      transcriptsById: {},
      transcriptLoadingById: {},
      transcriptErrorsById: {},
      promptBySession: {},
      subagents: {},
    });
  });

  it("merges the state update onto the spawn instead of replacing it", () => {
    // The spawn names the subagent and the state update only says how it ended.
    // Replacing one with the other leaves a row with no name.
    const { trackSubagent } = useAgentSessionsStore.getState();
    trackSubagent("s1", { id: "sub-1", name: "Explore", task: "map the repo" });
    trackSubagent("s1", { id: "sub-1", state: "completed" });

    const [subagent] = useAgentSessionsStore.getState().subagents.s1;
    expect(subagent.name).toBe("Explore");
    expect(subagent.task).toBe("map the repo");
    expect(subagent.state).toBe("completed");
  });

  it("drops a subagent that was killed rather than greying it out", () => {
    const { trackSubagent } = useAgentSessionsStore.getState();
    trackSubagent("s1", { id: "sub-1", name: "Explore" });
    trackSubagent("s1", { id: "sub-2", name: "Verify" });

    trackSubagent("s1", { id: "sub-1", state: "cancelled" });
    expect(useAgentSessionsStore.getState().subagents.s1.map((s) => s.id)).toEqual(["sub-2"]);

    // The session going away takes the rest of them with it.
    trackSubagent("s1", { id: "sub-2", state: "disconnected" });
    expect(useAgentSessionsStore.getState().subagents.s1).toEqual([]);
  });

  it("keeps the ones that ran to an end, so the dot can say how", () => {
    const { trackSubagent } = useAgentSessionsStore.getState();
    trackSubagent("s1", { id: "ok", name: "Explore" });
    trackSubagent("s1", { id: "bad", name: "Verify" });
    trackSubagent("s1", { id: "ok", state: "completed" });
    trackSubagent("s1", { id: "bad", state: "failed" });

    expect(useAgentSessionsStore.getState().subagents.s1.map((s) => s.state)).toEqual([
      "completed",
      "failed",
    ]);
  });

  it("forgets a session's subagents when the session is removed", () => {
    const { upsertSession, trackSubagent, removeSession } = useAgentSessionsStore.getState();
    upsertSession(session("s1"));
    trackSubagent("s1", { id: "sub-1", name: "Explore" });

    removeSession("s1");

    expect(useAgentSessionsStore.getState().subagents.s1).toBeUndefined();
  });

  it("keeps closed sessions in the history after the session list is reloaded", () => {
    const closed = { ...session("closed-1"), status: "closed" as const };

    useAgentSessionsStore.getState().setSessions([closed]);

    const state = useAgentSessionsStore.getState();
    expect(state.sessionIds).toEqual(["closed-1"]);
    expect(state.sessionsById["closed-1"]).toEqual(closed);
  });
});

describe("permission control-plane state", () => {
  beforeEach(() => {
    useAgentSessionsStore.setState({ pending: null, pendingQueue: [] });
  });

  it("publishes a pending permission synchronously", () => {
    const pending = {
      id: "permission-1",
      sessionId: "session-1",
      params: {},
      caller: "agent:claude",
      title: "Run command",
      depth: 0,
      createdAt: "2026-09-14T00:00:00Z",
      expiresAt: "2026-09-14T00:01:00Z",
    };

    useAgentSessionsStore.getState().enqueuePending(pending);

    expect(useAgentSessionsStore.getState().pending).toBe(pending);
    expect(useAgentSessionsStore.getState().pendingQueue).toEqual([pending]);
  });
});

describe("a session's own activity in the store", () => {
  beforeEach(() => {
    useAgentSessionsStore.setState({ sessionsById: {}, sessionIds: [], sessionsLoaded: false });
  });

  const idle = (id: string, over: Partial<AgentSession> = {}): AgentSession => ({ ...session(id), status: "idle", ...over });
  const row = (id: string) => useAgentSessionsStore.getState().sessionsById[id];

  it("takes it from the session list the backend returned", () => {
    useAgentSessionsStore.getState().setSessions([idle("a", { activity: { agentTurn: true, tasks: 1 } }), idle("b")]);
    expect(row("a").activity).toEqual({ agentTurn: true, tasks: 1 });
    expect(row("b").activity).toBeUndefined();
  });

  it("takes it from a single read that is upserted", () => {
    useAgentSessionsStore.getState().upsertSession(idle("a", { activity: { agentTurn: false, tasks: 2 } }));
    expect(row("a").activity).toEqual({ agentTurn: false, tasks: 2 });
  });

  it("is set by an event for a session it knows, replacing what was there", () => {
    const store = useAgentSessionsStore.getState();
    store.upsertSession(idle("a", { activity: { agentTurn: false, tasks: 2 } }));
    store.setActivity("a", { agentTurn: true, tasks: 1 });
    expect(row("a").activity).toEqual({ agentTurn: true, tasks: 1 });
  });

  it("is cleared by an event that says nothing runs", () => {
    const store = useAgentSessionsStore.getState();
    store.upsertSession(idle("a", { activity: { agentTurn: true, tasks: 0 } }));
    store.setActivity("a", { agentTurn: false, tasks: 0 });
    expect(row("a").activity).toBeUndefined();
  });

  it("ignores an event for a session it has not loaded", () => {
    useAgentSessionsStore.getState().setActivity("nobody", { agentTurn: true, tasks: 0 });
    expect(row("nobody")).toBeUndefined();
    expect(useAgentSessionsStore.getState().sessionIds).toEqual([]);
  });

  it("leaves the session object alone when the event changes nothing", () => {
    const store = useAgentSessionsStore.getState();
    store.upsertSession(idle("a", { activity: { agentTurn: true, tasks: 1 } }));
    const before = useAgentSessionsStore.getState().sessionsById;
    store.setActivity("a", { agentTurn: true, tasks: 1 });
    expect(useAgentSessionsStore.getState().sessionsById).toBe(before);
    store.upsertSession(idle("q"));
    const quiet = useAgentSessionsStore.getState().sessionsById;
    store.setActivity("q", { agentTurn: false, tasks: 0 });
    expect(useAgentSessionsStore.getState().sessionsById).toBe(quiet);
  });
});

describe("a read and an event of one session, in either order", () => {
  beforeEach(() => {
    useAgentSessionsStore.setState({ sessionsById: {}, sessionIds: [], sessionsLoaded: false });
  });
  const idle = (id: string, over: Partial<AgentSession> = {}): AgentSession => ({ ...session(id), status: "idle", ...over });
  const row = () => useAgentSessionsStore.getState().sessionsById.a;
  const ON = { agentTurn: true, tasks: 0 };

  it("a read that was in flight keeps the event that landed first", () => {
    const store = useAgentSessionsStore.getState();
    store.upsertSession(idle("a", { activity: { agentTurn: false, tasks: 0, rev: 2 } }));
    expect(store.setActivity("a", { ...ON, rev: 3 })).toBe(true);
    // The answer to a read made at rev 2 lands after the event of rev 3: no activity change at all.
    store.upsertSession(idle("a", { activity: { agentTurn: false, tasks: 0, rev: 2 } }));
    expect(row().activity).toEqual({ ...ON, rev: 3 });
    store.setSessions([idle("a", { activity: undefined })]);
    expect(row().activity).toEqual({ ...ON, rev: 3 });
    // Whatever else the read carries is taken.
    store.upsertSession(idle("a", { title: "renamed", activity: { agentTurn: false, tasks: 0, rev: 2 } }));
    expect(row().title).toBe("renamed");
    expect(row().activity).toEqual({ ...ON, rev: 3 });
  });

  it("a newer read replaces an older event, and a read of the same revision does too", () => {
    const store = useAgentSessionsStore.getState();
    store.upsertSession(idle("a"));
    store.setActivity("a", { ...ON, rev: 3 });
    store.upsertSession(idle("a", { activity: { agentTurn: false, tasks: 2, rev: 5 } }));
    expect(row().activity).toEqual({ agentTurn: false, tasks: 2, rev: 5 });
    store.setSessions([idle("a", { activity: { agentTurn: false, tasks: 1, rev: 5 } })]);
    expect(row().activity).toEqual({ agentTurn: false, tasks: 1, rev: 5 });
  });

  it("an event older than the row is dropped and says so", () => {
    const store = useAgentSessionsStore.getState();
    store.upsertSession(idle("a", { activity: { agentTurn: false, tasks: 2, rev: 5 } }));
    expect(store.setActivity("a", { ...ON, rev: 4 })).toBe(false);
    expect(row().activity).toEqual({ agentTurn: false, tasks: 2, rev: 5 });
    expect(store.setActivity("nobody", { ...ON, rev: 9 })).toBe(false);
  });

  it("keeps a revision with nothing running, so it can still order a read", () => {
    const store = useAgentSessionsStore.getState();
    store.upsertSession(idle("a"));
    store.setActivity("a", { ...ON, rev: 3 });
    store.setActivity("a", { agentTurn: false, tasks: 0, rev: 4, ended: true });
    expect(row().activity).toEqual({ agentTurn: false, tasks: 0, rev: 4, ended: true });
    store.upsertSession(idle("a", { activity: { ...ON, rev: 3 } }));
    expect(row().activity?.agentTurn).toBe(false);
  });

  it("a read with no activity at all cannot erase what an event set", () => {
    const store = useAgentSessionsStore.getState();
    store.upsertSession(idle("a"));
    store.setActivity("a", { ...ON, rev: 1 });
    store.upsertSession(idle("a"));
    expect(row().activity).toEqual({ ...ON, rev: 1 });
  });
});
