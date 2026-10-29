import { beforeEach, describe, expect, it } from "bun:test";
import { AppError } from "../../../bridge/errors";
import type { AgentSession } from "../../../bridge/agenthub";
import { useAgentSessionsStore } from "../../../stores/agentSessions";
import { prepareSessionConfig } from "./prepareSessionConfig";

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

const coded = (code: string, message = "x") => new AppError({ code, message, recoverable: true });

beforeEach(() => {
  useAgentSessionsStore.setState({ sessionsById: {}, sessionIds: [] });
});

describe("the session config query's call", () => {
  it("prepares the session and touches nothing else when it works", async () => {
    useAgentSessionsStore.getState().upsertSession(session());
    let statusReads = 0;
    const ok = await prepareSessionConfig("s1", {
      prepare: async () => {},
      getStatus: async () => { statusReads += 1; return session({ status: "closed" }); },
    });
    expect(ok).toBe(true);
    expect(statusReads).toBe(0);
    expect(useAgentSessionsStore.getState().sessionsById["s1"].status).toBe("idle");
  });

  it("answers a closed session by correcting the stale row, not by failing", async () => {
    // The window still shows idle: a parent agent closed the child through MCP.
    useAgentSessionsStore.getState().upsertSession(session({ status: "idle" }));
    const ok = await prepareSessionConfig("s1", {
      prepare: async () => { throw coded("agenthub.sessionClosed", "This session is closed. Send a message to reopen it."); },
      getStatus: async () => session({ status: "closed" }),
    });
    expect(ok).toBe(false); // resolved, so no error reaches the composer
    expect(useAgentSessionsStore.getState().sessionsById["s1"].status).toBe("closed");
  });

  it("still ends quietly when the row cannot be read again", async () => {
    useAgentSessionsStore.getState().upsertSession(session());
    const ok = await prepareSessionConfig("s1", {
      prepare: async () => { throw coded("agenthub.sessionClosed"); },
      getStatus: async () => { throw coded("agenthub.sessionNotFound"); },
    });
    expect(ok).toBe(false);
  });

  it("does not hide any other failure", async () => {
    useAgentSessionsStore.getState().upsertSession(session());
    let statusReads = 0;
    const failure = coded("validation_failed", "This session is closed. Send a message to reopen it.");
    await expect(
      prepareSessionConfig("s1", {
        prepare: async () => { throw failure; },
        getStatus: async () => { statusReads += 1; return session({ status: "closed" }); },
      }),
    ).rejects.toBe(failure);
    expect(statusReads).toBe(0);
    expect(useAgentSessionsStore.getState().sessionsById["s1"].status).toBe("idle");
  });
});
