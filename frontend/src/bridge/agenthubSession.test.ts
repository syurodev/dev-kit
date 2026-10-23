import { describe, expect, test } from "bun:test";
import type { AgentSession as RawSession } from "../../bindings/devkit/internal/agenthub/models";
import { toSession } from "./agenthub";

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
