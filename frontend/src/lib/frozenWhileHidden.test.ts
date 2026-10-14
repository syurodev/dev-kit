import { describe, expect, test } from "bun:test";
import { frozenWhileHidden } from "./frozenWhileHidden";

describe("frozenWhileHidden", () => {
  test("hands back what was built while visible, so React bails out of the hidden subtree", () => {
    const box = { current: null as string | null };
    expect(frozenWhileHidden(box, true, "prompt: a")).toBe("prompt: a");
    // Typed in Agent Hub while the companion is hidden: same element, no render.
    expect(frozenWhileHidden(box, false, "prompt: ab")).toBe("prompt: a");
    expect(frozenWhileHidden(box, false, "prompt: abc")).toBe("prompt: a");
  });

  test("catches up in one render when shown again", () => {
    const box = { current: "prompt: a" as string | null };
    expect(frozenWhileHidden(box, false, "prompt: abc")).toBe("prompt: a");
    expect(frozenWhileHidden(box, true, "prompt: abc")).toBe("prompt: abc");
  });

  test("mounted hidden, it still draws once", () => {
    const box = { current: null as string | null };
    expect(frozenWhileHidden(box, false, "first")).toBe("first");
    expect(frozenWhileHidden(box, false, "second")).toBe("first");
  });
});
