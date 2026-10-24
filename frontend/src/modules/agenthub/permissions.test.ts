import { describe, expect, it, mock } from "bun:test";

import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { AppError } from "@/bridge/errors";

const realBridge = await import("@/bridge/agenthub");
mock.module("@/bridge/agenthub", () => ({
  ...realBridge,
  agentHubGetStatus: async (id: string) => ({ id, status: "awaiting_permission" }) as AgentSession,
}));

import { useAgentSessionsStore } from "@/stores/agentSessions";
import { PERMISSION_NOT_WAITING, isPermissionNotWaiting, refreshSessionAfterPermission, statusAfterAnswer } from "./permissions";

const session = (status: AgentSession["status"]) => ({ id: "s1", status }) as AgentSession;

describe("statusAfterAnswer", () => {
  it("reads a status that still says waiting as running: the turn goes on", () => {
    // The read can land before Go puts the session back to running.
    expect(statusAfterAnswer(session("awaiting_permission"), false).status).toBe("running");
  });

  it("keeps waiting when the next request for the session has already arrived", () => {
    expect(statusAfterAnswer(session("awaiting_permission"), true).status).toBe("awaiting_permission");
  });

  it("leaves a turn that already ended as it ended", () => {
    for (const status of ["idle", "error", "closed", "running"] as const) {
      expect(statusAfterAnswer(session(status), false).status).toBe(status);
    }
  });
});

describe("a request Go no longer waits for", () => {
  it("is recognised by its published code, however it arrives", () => {
    expect(isPermissionNotWaiting(new AppError({ code: PERMISSION_NOT_WAITING, message: "gone", recoverable: true }))).toBe(true);
    expect(isPermissionNotWaiting({ cause: { code: PERMISSION_NOT_WAITING, message: "gone" } })).toBe(true);
    expect(isPermissionNotWaiting(new Error("boom"))).toBe(false);
  });

  it("is dequeued by the closed event's handling and the session read as running", async () => {
    const store = useAgentSessionsStore.getState();
    const request = { id: "p1", sessionId: "s1", params: {} } as unknown as PendingPermission;
    store.upsertSession({ id: "s1", status: "awaiting_permission" } as AgentSession);
    store.enqueuePending(request);
    // What AgentSessionsSubscriber does on agenthub:permissionClosed.
    useAgentSessionsStore.getState().dequeuePending("p1");
    await refreshSessionAfterPermission("s1");
    const after = useAgentSessionsStore.getState();
    expect(after.pendingQueue.some((p) => p.id === "p1")).toBe(false);
    expect(after.sessionsById["s1"]?.status).toBe("running");
  });
});
