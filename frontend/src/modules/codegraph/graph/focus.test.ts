import { describe, expect, it } from "bun:test";
import { flowThrough } from "./focus";

const link = (source: string, target: string) => ({ id: `${source}->${target}`, source, target });

// root → a → s → repo ; root → b → s ; a → repo (a shortcut past s) ; b → other
const links = [link("root", "a"), link("root", "b"), link("a", "s"), link("b", "s"), link("s", "repo"), link("a", "repo"), link("b", "other")];

describe("flowThrough", () => {
  it("lights what leads to the node and what it leads to, not its siblings' branches", () => {
    const focus = flowThrough("a", links);
    expect([...focus.nodes].sort()).toEqual(["a", "repo", "root", "s"]);
    expect(focus.nodes.has("b")).toBe(false);
    expect(focus.nodes.has("other")).toBe(false);
    expect([...focus.edges].sort()).toEqual(["a->repo", "a->s", "root->a", "s->repo"]);
  });

  it("keeps an edge between two lit nodes dim when it bypasses the hovered one", () => {
    const focus = flowThrough("s", links);
    expect(focus.nodes.has("a") && focus.nodes.has("repo")).toBe(true);
    expect(focus.edges.has("a->repo")).toBe(false);
    expect(focus.edges.has("b->s")).toBe(true);
    expect(focus.edges.has("b->other")).toBe(false);
  });

  it("ends on a cycle", () => {
    const focus = flowThrough("x", [link("x", "y"), link("y", "x")]);
    expect([...focus.nodes].sort()).toEqual(["x", "y"]);
    expect(focus.edges.size).toBe(2);
  });

  it("is just the node when nothing touches it", () => {
    expect([...flowThrough("lonely", links).nodes]).toEqual(["lonely"]);
  });
});
