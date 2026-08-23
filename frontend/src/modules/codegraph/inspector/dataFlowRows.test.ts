import { describe, expect, it } from "bun:test";
import { toDataFlowRows, WALL_LABELS, type DataFlowRow } from "./dataFlowRows";
import type { CodeGraphDataFlowStep } from "@/bridge/codegraph";

// flatMap, not filter: it narrows the discriminated union without depending on
// TypeScript inferring a type predicate from the callback.
const headers = (rows: DataFlowRow[]) => rows.flatMap((r) => (r.type === "header" ? [r] : []));
const events = (rows: DataFlowRow[]) => rows.flatMap((r) => (r.type === "event" ? [r] : []));

const step = (over: Partial<CodeGraphDataFlowStep>): CodeGraphDataFlowStep => ({
  nodeId: "n1",
  label: "OrderService.process",
  filePath: "OrderService.java",
  line: 10,
  kind: "read",
  depth: 0,
  targetId: "",
  unresolved: "",
  ...over,
});

describe("toDataFlowRows", () => {
  it("groups consecutive steps in the same method under one header", () => {
    const rows = toDataFlowRows([
      step({ nodeId: "n1", line: 10, kind: "write" }),
      step({ nodeId: "n1", line: 12, kind: "read" }),
      step({ nodeId: "n2", label: "Repo.save", line: 40, kind: "read" }),
    ]);
    expect(headers(rows)).toHaveLength(2);
    expect(headers(rows)[0].label).toBe("OrderService.process");
    expect(events(rows)).toHaveLength(3);
  });

  it("re-opens a header when the same method is revisited at a new depth", () => {
    const rows = toDataFlowRows([
      step({ nodeId: "n1", line: 10, depth: 0 }),
      step({ nodeId: "n2", label: "B.f", line: 20, depth: 1 }),
      step({ nodeId: "n1", line: 14, depth: 0 }),
    ]);
    expect(headers(rows)).toHaveLength(3);
  });

  it("marks a resolved call as navigable and an unresolved one as a wall", () => {
    const rows = toDataFlowRows([
      step({ kind: "call", targetId: "n2", line: 11 }),
      step({ kind: "call", targetId: "", unresolved: "library", line: 13 }),
    ]);
    const [first, second] = events(rows);
    expect(first.targetId).toBe("n2");
    expect(first.wall).toBe("");
    expect(second.targetId).toBe("");
    expect(second.wall).toBe(WALL_LABELS.library);
  });

  it("indents by depth", () => {
    const rows = toDataFlowRows([step({ depth: 3 })]);
    expect(rows[0].depth).toBe(3);
  });

  it("returns nothing for an empty trace", () => {
    expect(toDataFlowRows([])).toEqual([]);
  });
});
