import { describe, expect, it } from "bun:test";
import { canDrop, relativeDirection, type PaneParams } from "./layoutRules";

const note: PaneParams = { kind: "document", module: "notes", title: "Refund notes" };
const code: PaneParams = { kind: "context", module: "codegraph", title: "Code" };
const agent: PaneParams = { kind: "companion", title: "Agent" };

describe("canDrop", () => {
  it("keeps panes out of a document's tab strip but lets them split beside it", () => {
    expect(canDrop(code, { groupKind: "document", groupModule: "codegraph", location: "tab", position: "center" })).toBe(false);
    expect(canDrop(agent, { groupKind: "document", groupModule: "notes", location: "content", position: "center" })).toBe(false);
    expect(canDrop(agent, { groupKind: "document", groupModule: "notes", location: "content", position: "right" })).toBe(true);
  });

  it("lets a document join only its own module's group", () => {
    expect(canDrop(note, { groupKind: "document", groupModule: "notes", location: "tab", position: "center" })).toBe(true);
    expect(canDrop(note, { groupKind: "document", groupModule: "codegraph", location: "header_space", position: "center" })).toBe(false);
    expect(canDrop(note, { groupKind: "document", groupModule: "codegraph", location: "content", position: "right" })).toBe(true);
  });

  it("never lets a document into a pane group", () => {
    expect(canDrop(note, { groupKind: "context", groupModule: "codegraph", location: "content", position: "bottom" })).toBe(false);
    expect(canDrop(note, { groupKind: "companion", location: "tab", position: "center" })).toBe(false);
  });

  it("lets panes group with panes and anything reach the layout edges", () => {
    expect(canDrop(agent, { groupKind: "context", groupModule: "codegraph", location: "tab", position: "center" })).toBe(true);
    expect(canDrop(note, { location: "edge", position: "right" })).toBe(true);
  });
});

describe("relativeDirection", () => {
  const doc = { left: 0, top: 0, right: 600, bottom: 800 };
  it("reads right / below / within", () => {
    expect(relativeDirection({ left: 608, top: 0, right: 900, bottom: 800 }, doc)).toBe("right");
    expect(relativeDirection({ left: 0, top: 808, right: 600, bottom: 1000 }, doc)).toBe("below");
    expect(relativeDirection({ left: 10, top: 10, right: 500, bottom: 700 }, doc)).toBe("within");
  });
});
