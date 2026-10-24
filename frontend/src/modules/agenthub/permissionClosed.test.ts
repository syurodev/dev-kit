import { beforeEach, describe, expect, it, mock } from "bun:test";

import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { AppError } from "@/bridge/errors";

// The bridge's own listener runs over a captured Events.On, so a Go event is
// delivered the way the runtime delivers it.
const listeners = new Map<string, (ev: { data: unknown }) => void>();
const realRuntime = await import("@wailsio/runtime");
mock.module("@wailsio/runtime", () => ({
  ...realRuntime,
  Events: {
    ...realRuntime.Events,
    On: (name: string, cb: (ev: { data: unknown }) => void) => {
      listeners.set(name, cb);
      return () => listeners.delete(name);
    },
  },
}));

let statusReply: () => Promise<AgentSession> = async () => ({ id: "s1", status: "running" }) as AgentSession;
let resolveReply: () => Promise<void> = async () => {};
const realBridge = await import("@/bridge/agenthub");
mock.module("@/bridge/agenthub", () => ({
  ...realBridge,
  agentHubGetStatus: () => statusReply(),
  agentHubResolvePermission: () => resolveReply(),
}));

import { resetClosedPermissions, useAgentSessionsStore } from "@/stores/agentSessions";
import { handlePermissionClosed } from "@/workbench/AgentSessionsSubscriber";
import { PERMISSION_NOT_WAITING, answerPermission } from "./permissions";

const request = (id: string, sessionId = "s1") => ({ id, sessionId, params: {} }) as unknown as PendingPermission;
const status = () => useAgentSessionsStore.getState().sessionsById["s1"]?.status;
const queued = () => useAgentSessionsStore.getState().pendingQueue.map((p) => p.id);
const deferred = <T,>() => {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
};

beforeEach(() => {
  resetClosedPermissions();
  useAgentSessionsStore.setState({ pendingQueue: [], pending: null, sessionsById: {}, sessionIds: [] });
  useAgentSessionsStore.getState().upsertSession({ id: "s1", status: "awaiting_permission" } as AgentSession);
  statusReply = async () => ({ id: "s1", status: "running" }) as AgentSession;
  resolveReply = async () => {};
});

describe("agenthub:permissionClosed, delivered through the bridge listener", () => {
  it("takes the card down and reads the session as running", async () => {
    realBridge.onAgentHubPermissionClosed(handlePermissionClosed);
    useAgentSessionsStore.getState().enqueuePending(request("p1"));
    listeners.get("agenthub:permissionClosed")!({ data: [{ id: "p1", sessionId: "s1", reason: "withdrawn" }] });
    expect(queued()).toEqual([]);
    await new Promise((r) => setTimeout(r, 0));
    expect(status()).toBe("running");
  });

  it("leaves another request's card alone", () => {
    const store = useAgentSessionsStore.getState();
    store.enqueuePending(request("p1"));
    store.enqueuePending(request("p2"));
    handlePermissionClosed({ id: "p1", sessionId: "s1", reason: "turn_ended" });
    expect(queued()).toEqual(["p2"]);
  });

  it("does not let an open announced after its close bring the card back", () => {
    handlePermissionClosed({ id: "late", sessionId: "s1", reason: "turn_ended" });
    useAgentSessionsStore.getState().enqueuePending(request("late"));
    expect(queued()).toEqual([]);
  });

  it("does not let an older status read overwrite a newer request", async () => {
    const read = deferred<AgentSession>();
    statusReply = () => read.promise;
    const store = useAgentSessionsStore.getState();
    store.enqueuePending(request("old"));
    handlePermissionClosed({ id: "old", sessionId: "s1", reason: "withdrawn" });
    // The read began before the new request existed and says running.
    store.enqueuePending(request("new"));
    read.resolve({ id: "s1", status: "running" } as AgentSession);
    await new Promise((r) => setTimeout(r, 0));
    expect(queued()).toEqual(["new"]);
    expect(status()).toBe("awaiting_permission");
  });
});

describe("answering a request Go no longer waits for", () => {
  it("takes the card down, refreshes the session and shows no error", async () => {
    useAgentSessionsStore.getState().enqueuePending(request("p1"));
    resolveReply = async () => {
      throw new AppError({ code: PERMISSION_NOT_WAITING, message: "gone", recoverable: true });
    };
    expect(await answerPermission(request("p1"), null)).toBe("");
    expect(queued()).toEqual([]);
    expect(status()).toBe("running");
  });

  it("keeps the card and returns the message for any other failure", async () => {
    useAgentSessionsStore.getState().enqueuePending(request("p1"));
    resolveReply = async () => {
      throw new AppError({ code: "internal_error", message: "boom", recoverable: false });
    };
    expect(await answerPermission(request("p1"), "allow_once")).toBe("boom");
    expect(queued()).toEqual(["p1"]);
  });

  it("settles the card after a normal answer", async () => {
    useAgentSessionsStore.getState().enqueuePending(request("p1"));
    expect(await answerPermission(request("p1"), "allow_once")).toBe("");
    expect(queued()).toEqual([]);
    expect(status()).toBe("running");
  });
});
