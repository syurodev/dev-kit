import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { PendingPermission } from "@/bridge/agenthub";
import { AgentPermissionRequest } from "./AgentPermissionRequest";

function permission(overrides: Partial<PendingPermission> = {}): PendingPermission {
  return {
    id: "permission-1",
    sessionId: "session-1",
    params: {
      origin: "devkit_gate",
      env: "dev",
      locked: false,
      tool: "db.query",
      profileId: "profile-1",
      summary: "SELECT 1",
      toolCall: { title: "db.query", rawInput: { sql: "SELECT 1" } },
      options: [{ optionId: "allow_once", name: "Allow once" }],
    },
    caller: "agent:session-1",
    title: "Agent",
    depth: 0,
    createdAt: new Date().toISOString(),
    expiresAt: "",
    ...overrides,
  };
}

describe("scoped approval controls on the Agent Hub permission card", () => {
  it("shows bounded total-use and lifetime choices only for an eligible live gate", () => {
    const html = renderToString(
      <AgentPermissionRequest
        pending={permission({ scopedApprovalEligible: true })}
        onResolve={() => {}}
        onApproveScoped={() => {}}
      />,
    );

    expect(html).toContain('data-testid="permission-approve-scoped"');
    expect(html).toContain("Approve exact query");
    expect(html).toContain('aria-label="Total uses"');
    expect(html).toContain('aria-label="Approval lifetime"');
    for (const useCount of [1, 2, 5, 10]) expect(html).toContain(`value="${useCount}"`);
    for (const minutes of [10, 30, 60, 240, 480]) expect(html).toContain(`value="${minutes}"`);
    expect(html).toContain("4 hours");
    expect(html).toContain("8 hours");
  });

  it("hides the persistent approval action for locked, noneligible, and unmarked requests", () => {
    const locked = permission({
      scopedApprovalEligible: false,
      params: {
        origin: "devkit_gate",
        env: "prod",
        locked: true,
        tool: "db.query",
        profileId: "profile-1",
        summary: "SELECT 1",
      },
    });
    const offPath = permission({ scopedApprovalEligible: false });
    const legacy = permission();

    for (const pending of [locked, offPath, legacy]) {
      const html = renderToString(
        <AgentPermissionRequest
          pending={pending}
          onResolve={() => {}}
          onApproveScoped={() => {}}
        />,
      );
      expect(html).not.toContain('data-testid="permission-approve-scoped"');
    }
  });

  it("shows the create failure beside the answer controls", () => {
    const html = renderToString(
      <AgentPermissionRequest
        pending={permission({ scopedApprovalEligible: true })}
        onResolve={() => {}}
        onApproveScoped={() => {}}
        error="The approval could not be created."
      />,
    );

    expect(html).toContain("The approval could not be created.");
    expect(html).toContain('data-testid="permission-approve-scoped"');
  });
});
