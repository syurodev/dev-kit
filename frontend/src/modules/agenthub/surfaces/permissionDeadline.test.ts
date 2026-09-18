import { describe, expect, it } from "bun:test";

import { resolvePermissionDeadline } from "./usePermissionDeadline";

/**
 * The deadline belongs to the backend. Usually there is none — a permission
 * waits for the person, the way the vendors' own apps do — and these tests pin
 * both that and the rule that was wrong in the first implementation: a surface
 * mounting late must not restart the clock, which is the normal case for a
 * request that arrives while the rail is closed or the user is elsewhere.
 */
describe("permission deadline", () => {
  it("has none when the backend stamps no expiry", () => {
    const createdAt = new Date(Date.now() - 50_000).toISOString();

    expect(resolvePermissionDeadline({ createdAt, expiresAt: "" })).toBeNull();
    expect(resolvePermissionDeadline({ createdAt: "", expiresAt: "" })).toBeNull();
  });

  it("counts down to the backend's expiresAt, not 60s from mount", () => {
    // Arrived 45 seconds ago: 15 remain, not a fresh 60.
    const createdAt = new Date(Date.now() - 45_000).toISOString();
    const expiresAt = new Date(Date.now() + 15_000).toISOString();

    const deadline = resolvePermissionDeadline({ createdAt, expiresAt }) ?? 0;
    const secondsLeft = Math.max(0, Math.ceil((deadline - Date.now()) / 1000));

    expect(secondsLeft).toBeLessThanOrEqual(15);
    expect(secondsLeft).toBeGreaterThan(13);
  });

  it("reports an already-expired request as zero rather than restarting it", () => {
    const expiresAt = new Date(Date.now() - 5_000).toISOString();

    const deadline = resolvePermissionDeadline({ createdAt: "", expiresAt }) ?? 0;

    expect(Math.max(0, Math.ceil((deadline - Date.now()) / 1000))).toBe(0);
  });
});
