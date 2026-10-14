import { describe, expect, test } from "bun:test";
import { dismissOwnedLayers, topLayerOwnedBy } from "./ownedLayers";

/** Triggers in some subtree, and the popper wrappers the body holds, in open order. */
function scope(triggers: { controls: string }[]) {
  return {
    querySelectorAll: () => triggers.map((t) => ({ getAttribute: () => t.controls })),
  };
}

function doc(layers: { id: string; triggers?: { controls: string }[] }[]) {
  const escapes: number[] = [];
  const d = {
    layers,
    escapes,
    querySelectorAll: () =>
      d.layers.map((l) => ({ firstElementChild: { id: l.id, ...scope(l.triggers ?? []) } })),
  };
  return d;
}

const host = (controls: string[]) => scope(controls.map((c) => ({ controls: c })));

describe("topLayerOwnedBy", () => {
  test("the companion's own open menu is its own", () => {
    const d = doc([{ id: "menu-1" }]);
    expect(topLayerOwnedBy(host(["menu-1"]) as never, d as never)).toBe(true);
  });

  test("a submenu opened from inside its menu is its own too", () => {
    const d = doc([{ id: "menu-1", triggers: [{ controls: "sub-1" }] }, { id: "sub-1" }]);
    expect(topLayerOwnedBy(host(["menu-1"]) as never, d as never)).toBe(true);
  });

  test("a layer somebody else opened on top is not touched", () => {
    const d = doc([{ id: "menu-1" }, { id: "dialog-select" }]);
    expect(topLayerOwnedBy(host(["menu-1"]) as never, d as never)).toBe(false);
  });

  test("nothing open, nothing owned", () => {
    expect(topLayerOwnedBy(host(["menu-1"]) as never, doc([]) as never)).toBe(false);
  });
});

describe("dismissOwnedLayers", () => {
  test("presses Escape once per owned layer, a frame apart, and stops at a foreign one", () => {
    const d = doc([{ id: "other" }, { id: "menu-1", triggers: [{ controls: "sub-1" }] }, { id: "sub-1" }]);
    const frames: (() => void)[] = [];
    const escape = () => {
      d.escapes.push(1);
      d.layers.pop(); // the topmost layer closes
    };
    dismissOwnedLayers(host(["menu-1"]) as never, d as never, { escape, frame: (fn) => void frames.push(fn) });
    while (frames.length) frames.shift()!();
    expect(d.escapes.length).toBe(2);
    expect(d.layers.map((l) => l.id)).toEqual(["other"]);
  });

  test("gives up after a few tries if a layer will not close", () => {
    const d = doc([{ id: "menu-1" }]);
    const frames: (() => void)[] = [];
    dismissOwnedLayers(host(["menu-1"]) as never, d as never, { escape: () => d.escapes.push(1), frame: (fn) => void frames.push(fn) });
    while (frames.length) frames.shift()!();
    expect(d.escapes.length).toBe(4);
  });
});
