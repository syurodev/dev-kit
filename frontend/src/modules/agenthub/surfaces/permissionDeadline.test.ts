import { describe, expect, it } from "bun:test";

import { resolvePermissionDeadline } from "./usePermissionDeadline";

/**
 * The deadline belongs to the backend. These tests pin the two rules that were
 * wrong in the first implementation, both of which only bite when a surface
 * mounts late — which is the normal case for a request that arrives while the
 * rail is closed or the user is in another module.
 */
describe("permission deadline", () => {
  it("counts down to the backend's expiresAt, not 60s from mount", () => {
    // Arrived 45 seconds ago: 15 remain, not a fresh 60.
    const createdAt = new Date(Date.now() - 45_000).toISOString();
    const expiresAt = new Date(Date.now() + 15_000).toISOString();

    const deadline = resolvePermissionDeadline({ createdAt, expiresAt }, 60);
    const secondsLeft = Math.max(0, Math.ceil((deadline - Date.now()) / 1000));

    expect(secondsLeft).toBeLessThanOrEqual(15);
    expect(secondsLeft).toBeGreaterThan(13);
  });

  it("anchors to createdAt when the backend only stamps that", () => {
    const createdAt = new Date(Date.now() - 50_000).toISOString();

    const deadline = resolvePermissionDeadline({ createdAt, expiresAt: "" }, 60);
    const secondsLeft = Math.max(0, Math.ceil((deadline - Date.now()) / 1000));

    expect(secondsLeft).toBeLessThanOrEqual(10);
    expect(secondsLeft).toBeGreaterThan(8);
  });

  it("reports an already-expired request as zero rather than restarting it", () => {
    const expiresAt = new Date(Date.now() - 5_000).toISOString();

    const deadline = resolvePermissionDeadline({ createdAt: "", expiresAt }, 60);

    expect(Math.max(0, Math.ceil((deadline - Date.now()) / 1000))).toBe(0);
  });

  it("falls back to a full window only when nothing anchors it", () => {
    const deadline = resolvePermissionDeadline({ createdAt: "", expiresAt: "" }, 60);
    const secondsLeft = Math.ceil((deadline - Date.now()) / 1000);

    expect(secondsLeft).toBeGreaterThan(58);
    expect(secondsLeft).toBeLessThanOrEqual(60);
  });
});
