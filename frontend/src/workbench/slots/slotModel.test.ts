import { describe, expect, it } from "bun:test";
import {
  DEFAULT_PLACEMENT,
  changesLayout,
  dropTarget,
  menuTargets,
  move,
  requestedWidths,
  resolve,
  type Geometry,
  type Placement,
} from "./slotModel";

const all = { context: true, agent: true, terminal: true };

describe("resolve", () => {
  it("seats open panes where they were put", () => {
    expect(resolve(DEFAULT_PLACEMENT, all)).toEqual({ "a.top": "context", "a.bottom": "agent", bottom: "terminal" });
  });

  it("leaves closed panes out, so their slots take no space", () => {
    expect(resolve(DEFAULT_PLACEMENT, { agent: true })).toEqual({ "a.bottom": "agent" });
  });

  it("lets a companion keep its slot when a module's remembered context pane points there", () => {
    const p: Placement = { context: "b.top", agent: "b.top", terminal: "bottom" };
    // The context pane yields to the agent and takes the column's other half.
    expect(resolve(p, all)).toEqual({ "b.top": "agent", "b.bottom": "context", bottom: "terminal" });
  });
});

describe("context panes of two modules on screen", () => {
  const p: Placement = { agent: "a.bottom", terminal: "bottom", "context:notes": "a.top", "context:codegraph": "a.top" };
  const open = { agent: true, terminal: true, "context:notes": true, "context:codegraph": true };

  it("gives the first listed (the focused module's) its slot, and the other the next free one", () => {
    expect(resolve(p, open)).toEqual({
      "a.top": "context:notes",
      "a.bottom": "agent",
      "b.top": "context:codegraph",
      bottom: "terminal",
    });
  });

  it("swaps two modules' panes like any others", () => {
    const next = move(p, "context:codegraph", "a.top");
    expect(next["context:codegraph"]).toBe("a.top");
  });
});

describe("move", () => {
  it("swaps with whatever held the target slot", () => {
    expect(move(DEFAULT_PLACEMENT, "terminal", "a.top")).toEqual({ context: "bottom", agent: "a.bottom", terminal: "a.top" });
  });

  it("swaps with a closed pane too, so it cannot reopen on top of the newcomer", () => {
    const next = move(DEFAULT_PLACEMENT, "agent", "bottom");
    expect(next.terminal).toBe("a.bottom");
  });

  it("knows when a move would look the same", () => {
    // Agent alone in column A: sending it to an empty column B only relabels
    // the column it is drawn in.
    const p: Placement = { context: "a.top", agent: "a.top", terminal: "bottom" };
    expect(changesLayout(p, { agent: true }, "agent", "b.top")).toBe(false);
    expect(changesLayout(DEFAULT_PLACEMENT, all, "agent", "b.top")).toBe(true);
  });
});

// A 1440-wide row: document 296–896, column A 900–1424 split at 446, bottom
// row under the document from 600.
const geometry: Geometry = {
  row: { left: 4, top: 40, right: 1436, bottom: 946 },
  document: { left: 296, top: 40, right: 896, bottom: 596 },
  columns: { a: { left: 900, top: 40, right: 1436, bottom: 946 } },
  cards: {
    "a.top": { left: 900, top: 40, right: 1436, bottom: 492 },
    "a.bottom": { left: 900, top: 496, right: 1436, bottom: 946 },
    bottom: { left: 296, top: 600, right: 896, bottom: 946 },
  },
};
const drag = (x: number, y: number, pane: "context" | "agent" | "terminal" = "terminal") =>
  dropTarget({ placement: DEFAULT_PLACEMENT, open: all, pane, geometry, x, y });

describe("dropTarget", () => {
  it("offers a new column at the row's right edge", () => {
    expect(drag(1420, 300)).toMatchObject({ slot: "b.top", label: "New column" });
  });

  it("swaps with the pane under the pointer in a column", () => {
    expect(drag(1100, 200)).toMatchObject({ slot: "a.top", label: "Swap with Panel" });
    expect(drag(1100, 800)).toMatchObject({ slot: "a.bottom", label: "Swap with Agent" });
  });

  it("does nothing over the document body or the pane's own place", () => {
    expect(drag(500, 200)).toBeNull();
    expect(drag(500, 800)).toBeNull();
  });

  it("opens the bottom row from the document's foot when it is empty", () => {
    const open = { context: true, agent: true };
    const g = { ...geometry, document: { ...geometry.document, bottom: 946 }, cards: { ...geometry.cards, bottom: undefined } };
    expect(dropTarget({ placement: DEFAULT_PLACEMENT, open, pane: "agent", geometry: g, x: 500, y: 900 })).toMatchObject({
      slot: "bottom",
      label: "Below the document",
    });
  });

  it("splits a column holding one pane", () => {
    const open = { context: true, terminal: true };
    const g = { ...geometry, cards: { "a.top": geometry.columns.a, bottom: geometry.cards.bottom } };
    expect(dropTarget({ placement: DEFAULT_PLACEMENT, open, pane: "terminal", geometry: g, x: 1100, y: 800 })).toMatchObject({
      slot: "a.bottom",
      label: "Split below",
    });
  });
});

describe("menuTargets", () => {
  it("lists every destination and marks the current one", () => {
    const labels = menuTargets(DEFAULT_PLACEMENT, all, "terminal").map((t) => `${t.current ? "*" : ""}${t.label}`);
    expect(labels).toEqual([
      "*Below the document",
      "Right column · top · swap with Panel",
      "Right column · bottom · swap with Agent",
      "New column",
    ]);
  });
});

describe("requestedWidths", () => {
  const resolved = { "a.top": "context:notes", "b.top": "context:codegraph", bottom: "context:git" };

  it("takes a width only from a panel on its closed → open edge", () => {
    const panels = { notes: { width: 480 } };
    expect(requestedWidths({ modules: ["notes"], panels, wasOpen: new Set(), resolved })).toEqual({ a: 480 });
    expect(requestedWidths({ modules: ["notes"], panels, wasOpen: new Set(["notes"]), resolved })).toEqual({});
  });

  it("sizes the column the panel landed in, and never the bottom row", () => {
    const panels = { codegraph: { width: 520 }, git: { width: 600 } };
    expect(requestedWidths({ modules: ["codegraph", "git"], panels, wasOpen: new Set(), resolved })).toEqual({ b: 520 });
  });

  it("gives a contested column to the first listed module (the focused one)", () => {
    const both = { "a.top": "context:notes", "a.bottom": "context:tasks" };
    const panels = { notes: { width: 480 }, tasks: { width: 400 } };
    expect(requestedWidths({ modules: ["tasks", "notes"], panels, wasOpen: new Set(), resolved: both })).toEqual({ a: 400 });
  });

  it("ignores a panel that is not on screen", () => {
    expect(requestedWidths({ modules: ["kafka"], panels: { kafka: { width: 500 } }, wasOpen: new Set(), resolved })).toEqual({});
  });
});
