import { describe, expect, it } from "bun:test";

import { gettingStartedSteps, type GettingStartedActions } from "./GettingStarted";

const none = { workspace: false, codegraph: false, database: false, agent: false };

function recording() {
  const calls: string[] = [];
  const actions: GettingStartedActions = {
    newWorkspace: () => calls.push("newWorkspace"),
    openCodeGraph: () => calls.push("openCodeGraph"),
    addConnection: () => calls.push("addConnection"),
    newSession: () => calls.push("newSession"),
  };
  return { calls, actions };
}

describe("the first-run checklist", () => {
  it("names the four steps in the order each makes the next one better", () => {
    expect(gettingStartedSteps(none, recording().actions).map((step) => step.id)).toEqual([
      "workspace",
      "codegraph",
      "database",
      "agent",
    ]);
  });

  it("ticks a step from what exists, not from which button was pressed", () => {
    const steps = gettingStartedSteps({ ...none, workspace: true, agent: true }, recording().actions);
    expect(steps.filter((step) => step.done).map((step) => step.id)).toEqual(["workspace", "agent"]);
  });

  it("keeps each detail to one short line — the row truncates rather than wraps", () => {
    for (const step of gettingStartedSteps(none, recording().actions)) {
      expect(step.detail.length).toBeLessThanOrEqual(64);
    }
  });

  it("runs each step's own action", () => {
    const { calls, actions } = recording();
    for (const step of gettingStartedSteps(none, actions)) step.run();
    expect(calls).toEqual(["newWorkspace", "openCodeGraph", "addConnection", "newSession"]);
  });
});
