import { describe, expect, it } from "bun:test";
import { minimalChange } from "./minimalChange";

const apply = (before: string, c: { from: number; to: number; insert: string }) => before.slice(0, c.from) + c.insert + before.slice(c.to);

describe("minimalChange", () => {
  it("replaces only the middle that differs", () => {
    const before = "line1\nline2\nline3\n";
    const after = "line1\nLINE2\nline3\n";
    const c = minimalChange(before, after);
    expect(c).toEqual({ from: 6, to: 10, insert: "LINE" });
    expect(apply(before, c)).toBe(after);
  });

  it("handles insertion, deletion and identical text", () => {
    expect(minimalChange("abc", "abXc")).toEqual({ from: 2, to: 2, insert: "X" });
    expect(minimalChange("abXc", "abc")).toEqual({ from: 2, to: 3, insert: "" });
    expect(minimalChange("same", "same")).toEqual({ from: 0, to: 0, insert: "" });
    expect(apply("", minimalChange("", "new"))).toBe("new");
    expect(apply("old", minimalChange("old", ""))).toBe("");
  });

  it("does not split a surrogate pair", () => {
    // 😀 and 😁 share their high surrogate; the change must take both halves.
    const before = "a😀b";
    const after = "a😁b";
    const c = minimalChange(before, after);
    expect(apply(before, c)).toBe(after);
    expect(c.insert.length % 2).toBe(0);
  });

  it("reproduces any pair of strings", () => {
    const samples = ["", "a", "ab", "abab", "aXb", "😀😀", "x😀y", "\n\n", "line\r\nline"];
    for (const a of samples) for (const b of samples) expect(apply(a, minimalChange(a, b))).toBe(b);
  });
});
