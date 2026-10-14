import { describe, expect, test } from "bun:test";
import { companionPush } from "./AgentCompanionSlot";

describe("companionPush", () => {
  test("nothing is mounted until the companion is first shown", () => {
    expect(companionPush({ chatOpen: false, inAgentHub: false, mounted: false })).toEqual({ mounted: false, push: null });
    expect(companionPush({ chatOpen: true, inAgentHub: true, mounted: false })).toEqual({ mounted: false, push: null });
  });

  test("shown: mounted and visible", () => {
    expect(companionPush({ chatOpen: true, inAgentHub: false, mounted: false })).toEqual({ mounted: true, push: { hidden: false } });
  });

  test("closed after that: still mounted, hidden", () => {
    expect(companionPush({ chatOpen: false, inAgentHub: false, mounted: true })).toEqual({ mounted: true, push: { hidden: true } });
  });

  test("inside Agent Hub: still mounted, hidden, and back when leaving", () => {
    expect(companionPush({ chatOpen: true, inAgentHub: true, mounted: true })).toEqual({ mounted: true, push: { hidden: true } });
    expect(companionPush({ chatOpen: true, inAgentHub: false, mounted: true })).toEqual({ mounted: true, push: { hidden: false } });
  });
});
