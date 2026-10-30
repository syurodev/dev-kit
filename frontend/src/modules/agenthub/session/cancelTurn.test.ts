import { beforeEach, describe, expect, it, mock } from "bun:test";

import type { AgentSession } from "@/bridge/agenthub";

const calls: string[] = [];
let failCancel: Error | null = null;
const realBridge = await import("@/bridge/agenthub");
mock.module("@/bridge/agenthub", () => ({
  ...realBridge,
  agentHubCancel: async (id: string) => {
    calls.push(`cancel:${id}`);
    if (failCancel) throw failCancel;
  },
  agentHubGetStatus: async (id: string) => ({ id, status: "idle" }) as AgentSession,
}));

import { useAgentSessionsStore } from "@/stores/agentSessions";
import { cancelSessionTurn } from "./cancelTurn";

beforeEach(() => {
  calls.length = 0;
  failCancel = null;
  useAgentSessionsStore.setState({ sessionsById: { s1: { id: "s1", status: "running" } as AgentSession } });
});

describe("cancelSessionTurn", () => {
  it("cancels the session's turn, drops what waited on the person and reads the session again", async () => {
    const store = useAgentSessionsStore.getState();
    const dequeuePending = mock(store.dequeuePendingForSession);
    const dequeueElicitation = mock(store.dequeueElicitationForSession);
    useAgentSessionsStore.setState({ dequeuePendingForSession: dequeuePending, dequeueElicitationForSession: dequeueElicitation });
    const errors: unknown[] = [];
    await cancelSessionTurn("s1", (e) => errors.push(e));
    expect(calls).toEqual(["cancel:s1"]);
    expect(dequeuePending).toHaveBeenCalledWith("s1");
    expect(dequeueElicitation).toHaveBeenCalledWith("s1");
    expect(useAgentSessionsStore.getState().sessionsById.s1.status).toBe("idle");
    expect(errors).toEqual([]);
  });

  it("reports a failure instead of throwing, and leaves the session as it was", async () => {
    failCancel = new Error("no runtime");
    const errors: unknown[] = [];
    await cancelSessionTurn("s1", (e) => errors.push(e));
    expect(errors).toEqual([failCancel]);
    expect(useAgentSessionsStore.getState().sessionsById.s1.status).toBe("running");
  });
});
