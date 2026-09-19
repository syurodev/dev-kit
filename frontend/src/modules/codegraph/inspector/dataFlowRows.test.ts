import { describe, expect, it } from "bun:test";
import { filterFlow, summarizeFlow, toDataFlowGroups, WALL_LABELS } from "./dataFlowRows";
import type { CodeGraphDataFlowStep } from "@/bridge/codegraph";

const step = (over: Partial<CodeGraphDataFlowStep>): CodeGraphDataFlowStep => ({
  nodeId: "n1",
  label: "OrderService.process",
  filePath: "src/OrderService.java",
  line: 10,
  kind: "read",
  depth: 0,
  targetId: "",
  unresolved: "",
  ...over,
});

describe("toDataFlowGroups", () => {
  it("puts consecutive steps in the same method in one group", () => {
    const groups = toDataFlowGroups([
      step({ nodeId: "n1", line: 10, kind: "write" }),
      step({ nodeId: "n1", line: 12, kind: "read" }),
      step({ nodeId: "n2", label: "Repo.save", line: 40, kind: "read" }),
    ]);
    expect(groups.map((g) => g.label)).toEqual(["OrderService.process", "Repo.save"]);
    expect(groups[0].events.map((e) => e.kind)).toEqual(["write", "read"]);
    // Open file lands on the first line the method touched.
    expect(groups[0].line).toBe(10);
  });

  it("re-opens a group when the same method is revisited at a new depth", () => {
    const groups = toDataFlowGroups([
      step({ nodeId: "n1", line: 10, depth: 0 }),
      step({ nodeId: "n2", label: "B.f", line: 20, depth: 1 }),
      step({ nodeId: "n1", line: 14, depth: 0 }),
    ]);
    expect(groups).toHaveLength(3);
    expect(groups[1].depth).toBe(1);
  });

  it("names a call by its callee, and marks a wall instead of a target", () => {
    const [g] = toDataFlowGroups([
      step({ kind: "call", targetId: "n2", line: 11 }),
      step({ kind: "call", targetId: "", unresolved: "library", line: 13 }),
      step({ nodeId: "n2", label: "Repo.save", depth: 1 }),
    ]);
    expect(g.events[0].targetId).toBe("n2");
    expect(g.events[0].targetLabel).toBe("Repo.save");
    expect(g.events[1].targetId).toBe("");
    expect(g.events[1].wall).toBe(WALL_LABELS.library);
  });

  it("returns nothing for an empty trace", () => {
    expect(toDataFlowGroups([])).toEqual([]);
  });
});

describe("summarizeFlow and filterFlow", () => {
  const groups = toDataFlowGroups([
    step({ nodeId: "a", kind: "read" }),
    step({ nodeId: "a", kind: "call", targetId: "b" }),
    step({ nodeId: "b", label: "B.save", kind: "write", depth: 1 }),
    step({ nodeId: "c", label: "C.get", kind: "read", unresolved: "interface" }),
  ]);

  it("counts methods, not steps", () => {
    expect(summarizeFlow(groups)).toEqual({ readers: 2, writers: 1, walls: 1 });
  });

  it("keeps only the writers, and only their writes", () => {
    const writes = filterFlow(groups, "write");
    expect(writes.map((g) => g.nodeId)).toEqual(["b"]);
    expect(writes[0].events.map((e) => e.kind)).toEqual(["write"]);
  });

  it("leaves everything for all", () => {
    expect(filterFlow(groups, "all")).toBe(groups);
  });
});
