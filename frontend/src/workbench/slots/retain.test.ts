import { describe, expect, test } from "bun:test";
import { expireKept, retainStep, shownRecord, stillLeaving } from "./retain";

describe("retaining what a closing panel showed", () => {
  test("a key that leaves is kept for the close and no longer", () => {
    const kept = retainStep({ notes: "A", agent: "B" }, { agent: "B" }, {}, 1000, 120);
    expect(kept).toEqual({ notes: { value: "A", until: 1120 } });
    expect(shownRecord({ agent: "B" }, kept)).toEqual({ agent: "B", notes: "A" });
    expect(expireKept(kept, 1119)).toBe(kept);
    expect(expireKept(kept, 1120)).toEqual({});
  });

  test("the last value is what is kept, not a later one", () => {
    // NoteEditor re-pushes its panel on every edit: the close keeps the last push.
    const kept = retainStep({ notes: "edit 7" }, {}, {}, 0, 120);
    expect(kept.notes.value).toBe("edit 7");
  });

  test("reopening during the close drops the kept copy; live wins", () => {
    const kept = retainStep({ notes: "A" }, {}, {}, 0, 120);
    const again = retainStep({}, { notes: "A2" }, kept, 50, 120);
    expect(again).toEqual({});
    expect(shownRecord({ notes: "A2" }, again)).toEqual({ notes: "A2" });
  });

  test("a second close while the first is still leaving keeps both, each on its own clock", () => {
    const first = retainStep({ a: 1, b: 2 }, { b: 2 }, {}, 0, 120);
    const second = retainStep({ b: 2 }, {}, first, 80, 120);
    expect(second).toEqual({ a: { value: 1, until: 120 }, b: { value: 2, until: 200 } });
    expect(expireKept(second, 150)).toEqual({ b: { value: 2, until: 200 } });
  });

  test("an expired entry is not carried into the next step", () => {
    const kept = retainStep({ a: 1 }, {}, {}, 0, 120);
    expect(retainStep({}, {}, kept, 500, 120)).toEqual({});
  });

  test("Reduce Motion keeps nothing", () => {
    expect(retainStep({ notes: "A" }, {}, {}, 0, 0)).toEqual({});
  });
});

describe("stillLeaving: which kept panes a closing container may still draw", () => {
  test("a pane now on screen elsewhere is not drawn here", () => {
    const { shown } = stillLeaving({ top: "context:notes", bottom: "agent" }, new Set(["agent"]), new Set());
    expect(shown).toEqual({ top: "context:notes", bottom: null });
  });

  test("once seen elsewhere it stays gone, even after it closes there too", () => {
    // Move P from column A to the bottom row, then close it within 120 ms:
    // the column's kept P must not come back while the row keeps its own.
    const first = stillLeaving({ top: "P" }, new Set(["P"]), new Set());
    expect(first.shown).toEqual({ top: null });
    const second = stillLeaving({ top: "P" }, new Set(), first.seen);
    expect(second.shown).toEqual({ top: null });
  });

  test("nothing seen elsewhere, everything kept is drawn", () => {
    const { shown, seen } = stillLeaving({ top: "a", bottom: null }, new Set(), new Set());
    expect(shown).toEqual({ top: "a", bottom: null });
    expect(seen.size).toBe(0);
  });
});
