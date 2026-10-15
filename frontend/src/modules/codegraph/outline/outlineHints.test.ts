import { describe, expect, it } from "bun:test";
import { effectHint, exitMarker, foldHint, hiddenHint, implsHint, shownHint } from "./outlineHints";

describe("exitMarker", () => {
  it("names how the branch leaves, with what that means", () => {
    expect(exitMarker({ exits: true, exit: "return", frameKind: "if" })).toEqual({
      word: "returns",
      tone: "neutral",
      hint: "This branch ends here — the method returns. Nothing after it runs on this path.",
    });
    expect(exitMarker({ exits: true, exit: "throw", frameKind: "catch" })).toMatchObject({ word: "throws", tone: "warning" });
    expect(exitMarker({ exits: true, exit: "throw", frameKind: "catch" })!.hint).toContain("an exception is thrown");
    expect(exitMarker({ exits: true, exit: "break", frameKind: "if" })).toMatchObject({ word: "breaks" });
    expect(exitMarker({ exits: true, exit: "break", frameKind: "if" })!.hint).toContain("leaves the loop");
    expect(exitMarker({ exits: true, exit: "continue", frameKind: "if" })!.hint).toContain("skips to the next iteration");
  });

  it("says a break in a switch case leaves the switch", () => {
    expect(exitMarker({ exits: true, exit: "break", frameKind: "case" })!.hint).toContain("leaves the switch");
  });

  it("falls back to 'exits' for an index that recorded only that it leaves", () => {
    expect(exitMarker({ exits: true, frameKind: "catch" })).toMatchObject({ word: "exits", tone: "neutral" });
    expect(exitMarker({ exits: true, frameKind: "catch" })!.hint).toBe("This branch ends here. Nothing after it runs on this path.");
  });

  it("is nothing for a branch that falls through", () => {
    expect(exitMarker({ exits: false, frameKind: "if" })).toBeNull();
  });
});

describe("foldHint", () => {
  it("lists every reason with its count and says what a click does", () => {
    const counts = [{ reason: "logging", count: 2 }, { reason: "metrics", count: 1 }];
    expect(foldHint(3, counts, false)).toBe("3 hidden: 2 logging, 1 metrics — click to show");
    expect(foldHint(3, counts, true)).toBe("3 hidden: 2 logging, 1 metrics — click to hide");
    expect(foldHint(1, [{ reason: "helper", count: 1 }], false)).toBe("1 hidden: 1 helper — click to show");
  });
});

describe("row hints", () => {
  it("explains implementations, effects and hidden calls", () => {
    expect(implsHint(2)).toBe("Calls an abstract or overridable method; these 2 classes provide it, listed below");
    expect(implsHint(1)).toBe("Calls an abstract or overridable method; this class provides it, listed below");
    expect(effectHint({ kind: "kafka", role: "produce", target: "orders" })).toBe("Recognised Kafka produce to orders");
    expect(effectHint({ kind: "db", role: "read", target: "" })).toBe("Recognised DB read");
    expect(hiddenHint("logging")).toBe("Hidden by default: logging");
    expect(hiddenHint("you chose")).toBe("Hidden because you chose to hide it");
    expect(shownHint("decides a branch")).toBe("Shown: decides a branch");
    expect(shownHint("you chose")).toBe("Shown because you chose to show it");
    expect(shownHint("project")).toBeNull();
  });
});
