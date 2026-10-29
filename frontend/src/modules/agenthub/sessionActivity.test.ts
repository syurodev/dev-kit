import { describe, expect, it } from "bun:test";
import { backgroundTasksLabel, backgroundTasksOnly, isSessionQuiet, isWorking } from "./sessionActivity";
import type { AgentSession } from "../../bridge/agenthub";

const session = (over: Partial<AgentSession> = {}): AgentSession => ({
  id: "s1",
  agentType: "claude",
  projectPath: "/p",
  status: "idle",
  parentSessionId: null,
  depth: 0,
  title: "s1",
  acpSessionId: "",
  ...over,
});

describe("isWorking", () => {
  it("is true for a turn of ours (the stored status running)", () => {
    expect(isWorking(session({ status: "running" }))).toBe(true);
  });

  it("is true while the vendor runs a turn of its own, whatever the stored status", () => {
    expect(isWorking(session({ activity: { agentTurn: true, tasks: 0 } }))).toBe(true);
    expect(isWorking(session({ status: "running", activity: { agentTurn: true, tasks: 2 } }))).toBe(true);
  });

  it("is false for background tasks alone: nothing was asked for", () => {
    expect(isWorking(session({ activity: { agentTurn: false, tasks: 3 } }))).toBe(false);
  });

  it("is false for an idle, waiting, failed or closed session with no activity", () => {
    for (const status of ["idle", "awaiting_permission", "error", "closed", ""] as const) {
      expect(isWorking(session({ status }))).toBe(false);
    }
  });

  it("copes with no session", () => {
    expect(isWorking(undefined)).toBe(false);
  });
});

describe("backgroundTasksOnly", () => {
  it("counts the tasks of a session that is otherwise quiet", () => {
    expect(backgroundTasksOnly(session({ activity: { agentTurn: false, tasks: 2 } }))).toBe(2);
  });

  it("is zero while the session is working, so the card says one thing", () => {
    expect(backgroundTasksOnly(session({ status: "running", activity: { agentTurn: false, tasks: 2 } }))).toBe(0);
    expect(backgroundTasksOnly(session({ activity: { agentTurn: true, tasks: 2 } }))).toBe(0);
  });

  it("is zero for a closed session: its process, and so its tasks, are gone", () => {
    expect(backgroundTasksOnly(session({ status: "closed", activity: { agentTurn: false, tasks: 2 } }))).toBe(0);
  });

  it("is zero with no activity", () => {
    expect(backgroundTasksOnly(session())).toBe(0);
  });
});

describe("backgroundTasksLabel", () => {
  it("is plural-aware", () => {
    expect(backgroundTasksLabel(1)).toBe("1 background task");
    expect(backgroundTasksLabel(2)).toBe("2 background tasks");
  });
});

describe("isSessionQuiet", () => {
  it("is true for a loaded session that nothing is working on and nothing waits for", () => {
    for (const status of ["idle", "error", "closed"] as const) expect(isSessionQuiet(session({ status }), false)).toBe(true);
    expect(isSessionQuiet(session({ activity: { agentTurn: false, tasks: 3 } }), false)).toBe(true);
  });

  it("is false while a turn of ours runs or is in flight, the agent runs its own, or a permission waits", () => {
    expect(isSessionQuiet(session({ status: "running" }), false)).toBe(false);
    expect(isSessionQuiet(session(), true)).toBe(false);
    expect(isSessionQuiet(session({ activity: { agentTurn: true, tasks: 0 } }), false)).toBe(false);
    expect(isSessionQuiet(session({ status: "awaiting_permission" }), false)).toBe(false);
  });

  it("is false for a session that is not loaded: nothing is known, so nothing is settled", () => {
    expect(isSessionQuiet(undefined, false)).toBe(false);
  });
});
