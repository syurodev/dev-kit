import { describe, expect, it } from "bun:test";
import { createRequestGuard } from "./requestGuard";

describe("createRequestGuard", () => {
  it("keeps only the latest request under a key current", () => {
    const guard = createRequestGuard();
    const first = guard.begin("flow");
    expect(first()).toBe(true);
    const second = guard.begin("flow");
    // The slower first answer must not overwrite the second.
    expect(first()).toBe(false);
    expect(second()).toBe(true);
  });

  it("keys are independent", () => {
    const guard = createRequestGuard();
    const flow = guard.begin("flow");
    guard.begin("inspect");
    expect(flow()).toBe(true);
  });

  it("invalidateAll supersedes everything in flight, but not what starts after", () => {
    const guard = createRequestGuard();
    const flow = guard.begin("flow");
    const plan = guard.begin("plan");
    guard.invalidateAll();
    expect(flow()).toBe(false);
    expect(plan()).toBe(false);
    expect(guard.begin("flow")()).toBe(true);
  });
});
