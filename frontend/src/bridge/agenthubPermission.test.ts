import { beforeEach, describe, expect, it, mock } from "bun:test";

const listeners = new Map<string, (event: { data: unknown }) => void>();
const runtime = await import("@wailsio/runtime");
mock.module("@wailsio/runtime", () => ({
  ...runtime,
  Events: {
    ...runtime.Events,
    On: (name: string, handler: (event: { data: unknown }) => void) => {
      listeners.set(name, handler);
      return () => listeners.delete(name);
    },
  },
}));

let pendingRows: unknown[] | null = [];
mock.module("../../bindings/devkit/internal/bridge", () => ({
  AppService: {
    AgentHubPendingPermissions: async () => pendingRows,
  },
}));

const bridge = await import("./agenthub");

beforeEach(() => {
  listeners.clear();
  pendingRows = [];
});

describe("scoped approval eligibility through the Agent Hub bridge", () => {
  it("preserves only an explicit true from the live permission event", () => {
    const received: boolean[] = [];
    bridge.onAgentHubPermission((permission) => received.push(permission.scopedApprovalEligible === true));
    const emit = listeners.get("agenthub:permission");
    expect(emit).toBeDefined();

    emit!({ data: { id: "eligible", sessionId: "s1", scopedApprovalEligible: true } });
    emit!({ data: { id: "ineligible", sessionId: "s1", scopedApprovalEligible: false } });
    emit!({ data: { id: "legacy", sessionId: "s1" } });

    expect(received).toEqual([true, false, false]);
  });

  it("preserves eligibility when rebuilding pending permissions from the API", async () => {
    pendingRows = [
      { id: "eligible", sessionId: "s1", params: {}, scopedApprovalEligible: true },
      { id: "ineligible", sessionId: "s1", params: {}, scopedApprovalEligible: false },
      { id: "legacy", sessionId: "s1", params: {} },
    ];

    const permissions = await bridge.agentHubPendingPermissions();
    expect(permissions.map((permission) => [permission.id, permission.scopedApprovalEligible])).toEqual([
      ["eligible", true],
      ["ineligible", false],
      ["legacy", false],
    ]);
  });
});
