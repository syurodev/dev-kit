import { describe, expect, it } from "bun:test";
import type { CodeGraphNode, CodeGraphStep, CodeGraphSteps } from "@/bridge/codegraph";
import { buildOutline, type OutlineRow } from "./buildOutline";
import { cursorIndex, isRowOf, newKeys, nodeOf, pathTo, treeShape } from "./outlineNav";

const node = (id: string): CodeGraphNode => ({
  id, label: `acme.P.${id}`, kind: "method", layer: "", filePath: "P.java", line: 1, endLine: 9, summary: "",
});
const step = (over: Partial<CodeGraphStep>): CodeGraphStep => ({
  line: 1, col: 1, text: "x()", calleeKey: "#x", frames: [], visible: true, reason: "side effect", ...over,
});
const nodes = (...ids: string[]) => new Map(ids.map((id) => [id, node(id)]));
const call = (to: string, over: Partial<CodeGraphStep> = {}) => step({ text: `${to}()`, calleeKey: `acme.P#${to}`, to, ...over });
const outline = (steps: CodeGraphSteps["steps"], ids: string[], toggled: Record<string, boolean> = {}) =>
  buildOutline({ rootId: "a", nodes: nodes(...ids), steps: { legacy: false, truncated: [], steps }, toggled });

describe("isRowOf", () => {
  it("is the node's own rows, not the library calls inside it", () => {
    const rows = outline({ a: [call("b"), step({ text: "log.info()", calleeKey: "Log#info" })], b: [] }, ["a", "b"]);
    const [root, toB, lib] = rows;
    expect(isRowOf(root, "a")).toBe(true);
    expect(isRowOf(toB, "b")).toBe(true);
    expect(isRowOf(toB, "a")).toBe(false);
    // The library call acts on its method (Open in editor, Show in Graph) but is not it.
    expect(isRowOf(lib, "a")).toBe(false);
    expect(nodeOf(lib)).toBe("a");
  });
});

describe("cursorIndex", () => {
  const row = (key: string): OutlineRow => ({ kind: "more", key, depth: 0, nodeId: "a" });
  it("follows its row by key when rows open above it", () => {
    expect(cursorIndex([row("x"), row("y"), row("z")], "z", 2)).toBe(2);
    expect(cursorIndex([row("x"), row("new1"), row("new2"), row("y"), row("z")], "z", 2)).toBe(4);
  });
  it("keeps its index, in range, when its row is gone", () => {
    expect(cursorIndex([row("x"), row("y")], "gone", 1)).toBe(1);
    expect(cursorIndex([row("x")], "gone", 5)).toBe(0);
    expect(cursorIndex([], "x", 3)).toBe(0);
    expect(cursorIndex([row("x"), row("y")], null, 1)).toBe(1);
  });
});

describe("treeShape", () => {
  it("reads parents, levels and sibling positions off the depths", () => {
    const rows = outline({ a: [call("b"), call("c")], b: [call("d")], c: [], d: [] }, ["a", "b", "c", "d"]);
    // a; b() > d(); c()
    const shape = treeShape(rows);
    expect(shape.parent).toEqual([-1, 0, 1, 0]);
    expect(shape.level).toEqual([1, 2, 3, 2]);
    expect(shape.posinset).toEqual([1, 1, 1, 2]);
    expect(shape.setsize).toEqual([1, 2, 1, 2]);
    expect(shape.lasts).toEqual([[true], [true, false], [true, false, true], [true, true]]);
  });

  it("hangs a frame's steps off the frame heading", () => {
    const iff = { id: "2:5", kind: "if", text: "if ok" };
    const rows = outline({ a: [call("b", { frames: [iff] }), call("c", { frames: [iff] }), call("d")], b: [], c: [], d: [] }, ["a", "b", "c", "d"]);
    // a; if ok > b(), c(); d()
    expect(rows.map((r) => r.kind)).toEqual(["method", "frame", "call", "call", "call"]);
    const shape = treeShape(rows);
    expect(shape.parent).toEqual([-1, 0, 1, 1, 0]);
    expect(shape.lasts).toEqual([[true], [true, false], [true, false, false], [true, false, true], [true, true]]);
  });
});

describe("newKeys", () => {
  const row = (key: string): OutlineRow => ({ kind: "more", key, depth: 0, nodeId: "a" });
  it("is what opening a row revealed", () => {
    expect([...newKeys([row("a"), row("b")], [row("a"), row("a1"), row("a2"), row("b")])]).toEqual(["a1", "a2"]);
  });
  it("is nothing on the outline's first rows, on a collapse, or on the same rows again", () => {
    expect(newKeys([], [row("a"), row("b")]).size).toBe(0);
    expect(newKeys([row("a"), row("a1"), row("b")], [row("a"), row("b")]).size).toBe(0);
    expect(newKeys([row("a")], [row("a")]).size).toBe(0);
  });
});

describe("pathTo", () => {
  it("opens the closed rows above a node's first row", () => {
    // Depth 3: a, b and c have their bodies drawn; d's call is drawn closed, so e is not seen.
    const steps = { a: [call("b")], b: [call("c")], c: [call("d")], d: [call("e")], e: [] };
    const ids = ["a", "b", "c", "d", "e"];
    expect(outline(steps, ids).some((r) => isRowOf(r, "e"))).toBe(false);
    const keys = pathTo({ rootId: "a", nodes: nodes(...ids), steps: { legacy: false, truncated: [], steps }, toggled: {} }, "e");
    expect(keys).toHaveLength(4);
    expect(keys[0]).toBe("root");
    const opened = outline(steps, ids, Object.fromEntries(keys.map((k) => [k, true])));
    expect(opened.some((r) => isRowOf(r, "e"))).toBe(true);
  });

  it("opens a fold and the hidden call in it", () => {
    const steps = { a: [call("h", { visible: false, reason: "helper" })], h: [call("x")], x: [] };
    const ids = ["a", "h", "x"];
    const keys = pathTo({ rootId: "a", nodes: nodes(...ids), steps: { legacy: false, truncated: [], steps }, toggled: {} }, "x");
    const opened = outline(steps, ids, Object.fromEntries(keys.map((k) => [k, true])));
    expect(opened.map((r) => r.kind)).toEqual(["method", "fold", "call", "call"]);
    expect(isRowOf(opened[3], "x")).toBe(true);
  });

  it("is empty for a node the outline never reaches", () => {
    const steps = { a: [call("b")], b: [] };
    expect(pathTo({ rootId: "a", nodes: nodes("a", "b", "z"), steps: { legacy: false, truncated: [], steps }, toggled: {} }, "z")).toEqual([]);
  });
});
