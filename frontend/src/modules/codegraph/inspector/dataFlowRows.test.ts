import { describe, expect, it } from "bun:test";
import { describeUsage, focusOnField, splitOnField, toFieldUsage } from "./dataFlowRows";
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
  code: "",
  ...over,
});

describe("toFieldUsage", () => {
  it("sorts methods into writers and readers, each with its lines", () => {
    const usage = toFieldUsage([
      step({ nodeId: "a", label: "A.save", kind: "write", line: 3, code: "this.status = s;" }),
      step({ nodeId: "a", label: "A.save", kind: "read", line: 5 }),
      step({ nodeId: "b", label: "B.get", kind: "read", line: 8 }),
      step({ nodeId: "b", label: "B.get", kind: "read", line: 9 }),
    ]);
    expect(usage.writers.map((u) => u.label)).toEqual(["A.save"]);
    expect(usage.writers[0].accesses).toEqual([{ line: 3, code: "this.status = s;" }]);
    // A method that does both is in both lists, with the lines of each.
    expect(usage.readers.map((u) => [u.label, u.accesses.map((a) => a.line)])).toEqual([
      ["A.save", [5]],
      ["B.get", [8, 9]],
    ]);
  });

  it("says which call led the trace to a method", () => {
    const usage = toFieldUsage([
      step({ nodeId: "a", label: "Ctl.list", kind: "call", targetId: "b" }),
      step({ nodeId: "b", label: "Svc.find", kind: "read", depth: 1 }),
    ]);
    expect(usage.readers[0].via).toBe("Ctl.list");
  });

  it("reports calls it could not follow, but not ones already on the list", () => {
    const usage = toFieldUsage([
      step({ kind: "call", unresolved: "interface" }),
      step({ kind: "call", unresolved: "repeat" }),
      step({ kind: "call", unresolved: "cycle" }),
    ]);
    expect(usage.gaps).toHaveLength(1);
    expect(usage.gaps[0].reason.startsWith("interface")).toBe(true);
  });
});

describe("describeUsage", () => {
  const user = { nodeId: "x", label: "X.y", filePath: "", via: "", accesses: [] };
  it("calls a field nothing writes a constant", () => {
    expect(describeUsage("HEADER", { writers: [], readers: [user, user], gaps: [] })).toBe(
      "HEADER is read in 2 methods and never written — it behaves as a constant.",
    );
  });
  it("counts both sides", () => {
    expect(describeUsage("state", { writers: [user], readers: [user], gaps: [] })).toBe(
      "state is written in 1 method and read in 1 method.",
    );
  });
});

describe("splitOnField", () => {
  it("lights up the field as a whole word only", () => {
    expect(splitOnField("req.getHeader(DEVICE_HEADER)", "DEVICE_HEADER")).toEqual([
      { text: "req.getHeader(", hit: false },
      { text: "DEVICE_HEADER", hit: true },
      { text: ")", hit: false },
    ]);
    expect(splitOnField("userId = id", "id").filter((p) => p.hit)).toHaveLength(1);
  });
});

describe("focusOnField", () => {
  it("cuts a long line from the left so the field shows", () => {
    const code = "String deviceId = request.getHeader(DeviceHeaders.DEVICE_HEADER);";
    expect(focusOnField(code, "DEVICE_HEADER", 10)).toBe("…ceHeaders.DEVICE_HEADER);");
  });
  it("leaves a line alone when the field is near its start", () => {
    expect(focusOnField("DEVICE_HEADER.length()", "DEVICE_HEADER")).toBe("DEVICE_HEADER.length()");
  });
});
