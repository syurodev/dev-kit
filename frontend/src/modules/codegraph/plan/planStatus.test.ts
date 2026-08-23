import { describe, expect, it } from "bun:test";
import { PLAN_STATUS, planStatusOrder, aggregateLabel } from "./planStatus";
import type { PlanNodeStatus } from "@/bridge/codegraphPlan";

const ALL: PlanNodeStatus[] = [
  "not_implemented",
  "pending_enhancement",
  "implemented_matching",
  "implemented_diverged",
];

describe("PLAN_STATUS", () => {
  it("covers every status the backend can return", () => {
    for (const s of ALL) {
      expect(PLAN_STATUS[s]).toBeDefined();
      expect(PLAN_STATUS[s].label.length).toBeGreaterThan(0);
      expect(PLAN_STATUS[s].hint.length).toBeGreaterThan(0);
    }
  });

  it("never encodes status with a layer accent colour", () => {
    // The four layer accents are status/info, status/success, status/warning
    // and text/tertiary. A plan node using any of them for STATUS would say
    // the same thing twice, in conflict with the layer icon beside it.
    const banned = ["status-info", "status-success", "status-warning", "text-tertiary"];
    for (const s of ALL) {
      const styles = `${PLAN_STATUS[s].border} ${PLAN_STATUS[s].badge}`;
      for (const token of banned) {
        expect(styles).not.toContain(token);
      }
    }
  });

  it("gives every status a distinct label", () => {
    const labels = new Set(ALL.map((s) => PLAN_STATUS[s].label));
    expect(labels.size).toBe(ALL.length);
  });

  it("sorts diverged first and matching last, so problems surface", () => {
    const sorted = [...ALL].sort((a, b) => planStatusOrder(a) - planStatusOrder(b));
    expect(sorted[0]).toBe("implemented_diverged");
    expect(sorted[sorted.length - 1]).toBe("implemented_matching");
  });

  it("gives every status a unique sort key", () => {
    expect(new Set(ALL.map(planStatusOrder)).size).toBe(ALL.length);
  });
});

describe("aggregateLabel", () => {
  it("names all three aggregates", () => {
    expect(aggregateLabel("not_started")).toBe("Not started");
    expect(aggregateLabel("in_progress")).toBe("In progress");
    expect(aggregateLabel("complete")).toBe("Complete");
  });
});
