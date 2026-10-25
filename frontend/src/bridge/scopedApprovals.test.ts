import { beforeEach, describe, expect, it, mock } from "bun:test";
import type { ScopedApproval } from "./interaction";

let listed: ScopedApproval[] | null = null;
let revoked = "";

describe("scoped approval bridge adapters", () => {
  beforeEach(() => {
    listed = null;
    revoked = "";
    mock.module("../../bindings/devkit/internal/bridge", () => ({
      AppService: {
        InteractionScopedApprovals: async () => listed,
        InteractionRevokeScopedApproval: async (id: string) => { revoked = id; },
      },
    }));
  });

  async function bridge() {
    return import("./interaction");
  }

  it("normalizes the backend's null list to an empty list", async () => {
    const { getScopedApprovals } = await bridge();
    expect(await getScopedApprovals()).toEqual([]);
  });

  it("returns active approval rows and revokes by their returned id", async () => {
    const rows: ScopedApproval[] = [{
      id: "approval-1",
      caller: "agent:session-1",
      tool: "db.query",
      profileId: "profile-1",
      env: "dev",
      fingerprint: "a".repeat(64),
      createdAt: "2026-10-25T00:00:00Z",
      expiresAt: "2026-10-25T00:30:00Z",
      remaining: 2,
    }];
    listed = rows;
    const { getScopedApprovals, revokeScopedApproval } = await bridge();

    expect(await getScopedApprovals()).toEqual(rows);
    await revokeScopedApproval("approval-1");
    expect(revoked).toBe("approval-1");
  });
});
