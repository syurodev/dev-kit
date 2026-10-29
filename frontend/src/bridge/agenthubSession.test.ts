import { describe, expect, test } from "bun:test";
import type { AgentSession as RawSession } from "../../bindings/devkit/internal/agenthub/models";
import { toActivity, toSession } from "./agenthub";

const raw = (over: Partial<RawSession> = {}) => ({ ID: "s1", AgentType: "claude", ...over }) as RawSession;

describe("toSession", () => {
  test("reads the account the session runs as from the bound AccountID", () => {
    expect(toSession(raw({ AccountID: "claude-work" })).accountId).toBe("claude-work");
  });

  test("a session on the built-in account has an empty one", () => {
    expect(toSession(raw()).accountId).toBe("");
    expect(toSession(raw({ AccountID: "" })).accountId).toBe("");
  });
});

describe("a session's own activity from the row", () => {
  test("is carried when the vendor runs a turn or tasks of its own", () => {
    expect(toSession(raw({ activity: { agentTurn: true, tasks: 2 } })).activity).toEqual({ agentTurn: true, tasks: 2 });
    expect(toSession(raw({ activity: { tasks: 1 } })).activity).toEqual({ agentTurn: false, tasks: 1 });
    expect(toSession(raw({ activity: { agentTurn: true } })).activity).toEqual({ agentTurn: true, tasks: 0 });
  });

  test("keeps its revision when nothing runs, and what it ordered", () => {
    expect(toSession(raw({ activity: { rev: 4 } })).activity).toEqual({ agentTurn: false, tasks: 0, rev: 4 });
    expect(toSession(raw({ activity: { agentTurn: true, tasks: 1, rev: 7 } })).activity).toEqual({ agentTurn: true, tasks: 1, rev: 7 });
  });

  test("is absent, not an empty object, when it does nothing of its own", () => {
    expect(toSession(raw()).activity).toBeUndefined();
    expect(toSession(raw({ activity: null })).activity).toBeUndefined();
    expect(toSession(raw({ activity: {} })).activity).toBeUndefined();
    expect(toSession(raw({ activity: { agentTurn: false, tasks: 0 } })).activity).toBeUndefined();
  });
});

describe("the activity event's payload", () => {
  test("is the whole state, zero when it says nothing runs", () => {
    expect(toActivity({ agentTurn: true, tasks: 0 })).toEqual({ agentTurn: true, tasks: 0 });
    expect(toActivity({ agentTurn: false, tasks: 3 })).toEqual({ agentTurn: false, tasks: 3 });
    expect(toActivity({ agentTurn: false, tasks: 0 })).toEqual({ agentTurn: false, tasks: 0 });
    expect(toActivity({})).toEqual({ agentTurn: false, tasks: 0 });
    expect(toActivity({ agentTurn: "yes", tasks: -4 })).toEqual({ agentTurn: false, tasks: 0 });
  });

  test("carries the revision, and says explicitly whether the turn ended", () => {
    expect(toActivity({ agentTurn: false, tasks: 0, rev: 6, ended: true })).toEqual({ agentTurn: false, tasks: 0, rev: 6, ended: true });
    expect(toActivity({ agentTurn: false, tasks: 0, rev: 6, ended: false })).toEqual({ agentTurn: false, tasks: 0, rev: 6, ended: false });
    expect(toActivity({ agentTurn: true, tasks: 0, rev: 0 })).toEqual({ agentTurn: true, tasks: 0 });
  });
});
