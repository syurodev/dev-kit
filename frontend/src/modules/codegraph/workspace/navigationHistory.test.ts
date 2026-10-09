import { describe, expect, it } from "bun:test";
import {
  back,
  canBack,
  canForward,
  capture,
  current,
  emptyHistory,
  forward,
  MAX_HISTORY,
  push,
  type NavHistory,
  type NavPlace,
} from "./navigationHistory";

const at = (folderId: string, path: string, line: number, extra: Partial<NavPlace> = {}): NavPlace => ({
  workspaceId: "w",
  folderId,
  path,
  line,
  column: 1,
  ...extra,
});

/** A jump: capture where we were, then record where we arrived. */
const jump = (h: NavHistory, from: NavPlace, to: NavPlace) => push(capture(h, from), to);

describe("navigation history", () => {
  it("returns across folders to the exact place that was left", () => {
    let h = emptyHistory();
    h = jump(h, at("a", "src/x.ts", 10, { column: 7, scrollTop: 120 }), at("b", "src/y.ts", 3));
    const step = back(capture(h, at("b", "src/y.ts", 3)));
    expect(step?.target).toEqual(at("a", "src/x.ts", 10, { column: 7, scrollTop: 120 }));
    const fwd = forward(capture(step!.history, at("a", "src/x.ts", 10, { column: 9 })));
    expect(fwd?.target.folderId).toBe("b");
  });

  it("a new jump after Back cuts the Forward branch", () => {
    let h = emptyHistory();
    h = jump(h, at("a", "1.ts", 1), at("a", "2.ts", 1));
    h = jump(h, at("a", "2.ts", 1), at("a", "3.ts", 1));
    const back1 = back(capture(h, at("a", "3.ts", 1)))!;
    expect(current(back1.history)?.path).toBe("2.ts");
    const branched = jump(back1.history, at("a", "2.ts", 1), at("a", "4.ts", 1));
    expect(canForward(branched)).toBe(false);
    expect(branched.places.map((p) => p.path)).toEqual(["1.ts", "2.ts", "4.ts"]);
  });

  it("the same line of the same file is one place; another line of it is another", () => {
    let h = emptyHistory();
    h = jump(h, at("a", "f.ts", 5), at("a", "g.ts", 8));
    h = jump(h, at("a", "g.ts", 8), at("a", "g.ts", 8, { column: 4 })); // repeated navigation to the same spot
    expect(h.places).toHaveLength(2);
    h = jump(h, at("a", "g.ts", 8), at("a", "g.ts", 40));
    expect(h.places.map((p) => `${p.path}:${p.line}`)).toEqual(["f.ts:5", "g.ts:8", "g.ts:40"]);
  });

  it("two files of the same name in different folders are different places", () => {
    let h = emptyHistory();
    h = jump(h, at("a", "src/config.ts", 1), at("b", "src/config.ts", 1));
    expect(h.places).toHaveLength(2);
  });

  it("Back/Forward replay moves the pointer and records nothing", () => {
    let h = emptyHistory();
    h = jump(h, at("a", "1.ts", 1), at("a", "2.ts", 1));
    h = jump(h, at("a", "2.ts", 1), at("a", "3.ts", 1));
    const b = back(capture(h, at("a", "3.ts", 1)))!;
    expect(b.history.places).toHaveLength(3);
    expect(canBack(b.history)).toBe(true);
    expect(canForward(b.history)).toBe(true);
  });

  it("keeps at most MAX_HISTORY places, dropping the oldest", () => {
    let h = emptyHistory();
    for (let i = 0; i < MAX_HISTORY + 50; i++) h = push(h, at("a", `f${i}.ts`, 1));
    expect(h.places).toHaveLength(MAX_HISTORY);
    expect(h.places[0].path).toBe("f50.ts");
    expect(current(h)?.path).toBe(`f${MAX_HISTORY + 49}.ts`);
  });

  it("skips places that are gone (a deleted file, a removed folder) instead of failing", () => {
    let h = emptyHistory();
    h = jump(h, at("a", "1.ts", 1), at("gone", "2.ts", 1));
    h = jump(h, at("gone", "2.ts", 1), at("a", "3.ts", 1));
    const step = back(capture(h, at("a", "3.ts", 1)), (p) => p.folderId !== "gone");
    expect(step?.target.path).toBe("1.ts");
    expect(back(capture(h, at("a", "3.ts", 1)), () => false)).toBeNull();
  });

  it("Back from a place the history never saw lands on the latest recorded one", () => {
    let h = emptyHistory();
    h = jump(h, at("a", "1.ts", 1), at("a", "2.ts", 1));
    // The group is now showing 9.ts, opened from the sidebar.
    const step = back(capture(h, at("a", "9.ts", 1)));
    expect(step?.target.path).toBe("2.ts");
  });

  it("has nowhere to go when empty", () => {
    expect(back(emptyHistory())).toBeNull();
    expect(forward(emptyHistory())).toBeNull();
    expect(canBack(emptyHistory())).toBe(false);
  });
});
