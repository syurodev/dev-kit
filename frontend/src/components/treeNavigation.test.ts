import { describe, expect, it } from "bun:test";
import { flattenTree, type TreeNodeData } from "./tree-view";
import { treeKeyboardTarget, treePath } from "./treeNavigation";

const nodes: TreeNodeData[] = [
  { id: "src/main", label: "src/main", expandable: true, expanded: true, children: [
    { id: "src/main/a.ts", label: "a.ts" },
    { id: "src/main/empty", label: "empty", expandable: true, expanded: true, loading: true },
    { id: "src/main/b.ts", label: "b.ts" },
  ] },
  { id: "test", label: "test", expandable: true, expanded: false, children: [{ id: "test/a.ts", label: "a.ts" }] },
];
const rows = flattenTree(nodes);

describe("IDE file tree navigation", () => {
  it("skips loading placeholders while moving in either direction", () => {
    expect(treeKeyboardTarget(rows, 2, "ArrowDown")).toBe(4);
    expect(treeKeyboardTarget(rows, 4, "ArrowUp")).toBe(2);
  });
  it("moves into an expanded branch and back to the parent", () => {
    expect(treeKeyboardTarget(rows, 0, "ArrowRight")).toBe(1);
    expect(treeKeyboardTarget(rows, 4, "ArrowLeft")).toBe(0);
    expect(treeKeyboardTarget(rows, 5, "ArrowRight")).toBeNull();
  });
  it("stops at the edges and supports Home and End", () => {
    expect(treeKeyboardTarget(rows, 0, "ArrowUp")).toBe(0);
    expect(treeKeyboardTarget(rows, 5, "ArrowDown")).toBe(5);
    expect(treeKeyboardTarget(rows, 4, "Home")).toBe(0);
    expect(treeKeyboardTarget(rows, 0, "End")).toBe(5);
  });
  it("does not navigate into the placeholder of an empty folder", () => {
    expect(treeKeyboardTarget(rows, 2, "ArrowRight")).toBeNull();
  });
  it("finds collapsed ancestors and compact folder identities for reveal", () => {
    expect(treePath(nodes, "test/a.ts")?.map((node) => node.id)).toEqual(["test", "test/a.ts"]);
    expect(treePath(nodes, "src/main/a.ts")?.map((node) => node.id)).toEqual(["src/main", "src/main/a.ts"]);
    expect(treePath(nodes, "missing.ts")).toBeNull();
  });
});
