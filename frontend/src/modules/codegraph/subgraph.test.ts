import { describe, expect, it } from "bun:test";
import { mergeSubgraph } from "./subgraph";
import type { CodeGraphNode } from "@/bridge/codegraph";

const node = (id: string): CodeGraphNode => ({
  id,
  label: id,
  kind: "method",
  layer: "",
  filePath: "",
  line: 0,
  endLine: 0,
  summary: "",
});

describe("mergeSubgraph", () => {
  it("de-duplicates nodes and edges and takes paging state from the new page", () => {
    const merged = mergeSubgraph(
      {
        nodes: [node("a"), node("b")],
        edges: [{ from: "a", to: "b", kind: "call" }],
        truncated: true,
        continuation: "t1",
      },
      {
        nodes: [node("b"), node("c")],
        edges: [
          { from: "a", to: "b", kind: "call" },
          { from: "b", to: "c", kind: "call" },
        ],
        truncated: false,
      },
    );
    expect(merged.nodes.map((n) => n.id)).toEqual(["a", "b", "c"]);
    expect(merged.edges).toHaveLength(2);
    expect(merged.truncated).toBe(false);
    expect(merged.continuation).toBeUndefined();
  });
});
