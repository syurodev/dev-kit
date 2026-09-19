import { describe, expect, it } from "bun:test";
import { ChangeSet, Text } from "@codemirror/state";
import { baselineLineAt, identifierAt, type CodeIntelBaseline } from "./codeIntel";

describe("identifierAt", () => {
  it("finds the identifier the position is inside of", () => {
    expect(identifierAt("foo.bar", 1)).toEqual({ from: 0, to: 3, text: "foo" });
    expect(identifierAt("foo.bar", 5)).toEqual({ from: 4, to: 7, text: "bar" });
  });

  it("counts the slot right after an identifier as touching it", () => {
    // Right after "foo", before the ".": a click there should still resolve
    // to "foo", the same way most editors let you select a word by clicking
    // just past its last letter.
    expect(identifierAt("foo.bar", 3)).toEqual({ from: 0, to: 3, text: "foo" });
  });

  it("counts the slot right before an identifier as touching it", () => {
    expect(identifierAt("foo.bar", 4)).toEqual({ from: 4, to: 7, text: "bar" });
  });

  it("resolves to the touching identifier at the end of the line", () => {
    expect(identifierAt("foo", 3)).toEqual({ from: 0, to: 3, text: "foo" });
  });

  it("rejects a position with whitespace on both sides", () => {
    expect(identifierAt("foo  bar", 4)).toBeNull();
  });

  it("rejects an empty line", () => {
    expect(identifierAt("", 0)).toBeNull();
  });

  it("rejects a position past the end of the line", () => {
    expect(identifierAt("foo", 10)).toBeNull();
  });

  it("treats a pure number as not an identifier", () => {
    expect(identifierAt("123", 1)).toBeNull();
  });

  it("allows underscore and dollar starts", () => {
    expect(identifierAt("_private", 0)).toEqual({ from: 0, to: 8, text: "_private" });
    expect(identifierAt("$scope", 3)).toEqual({ from: 0, to: 6, text: "$scope" });
  });

  it("is unicode-aware", () => {
    expect(identifierAt("let café = 1", 5)).toEqual({ from: 4, to: 8, text: "café" });
  });
});

describe("baselineLineAt", () => {
  it("maps a current position back to its baseline line across edits made since the baseline", () => {
    const baselineDoc = Text.of(["first", "second", "target", "fourth"]);
    // Two lines inserted above everything — "target" used to be line 3.
    const sinceBaseline = ChangeSet.of({ from: 0, insert: "one\ntwo\n" }, baselineDoc.length);
    const baseline: CodeIntelBaseline = { doc: baselineDoc, sinceBaseline };

    const currentDoc = sinceBaseline.apply(baselineDoc);
    const targetLine = currentDoc.line(5);
    expect(targetLine.text).toBe("target");

    expect(baselineLineAt(baseline, targetLine.from)).toBe(3);
  });

  it("equals the display line right after a baseline reset", () => {
    const doc = Text.of(["alpha", "beta", "gamma"]);
    const baseline: CodeIntelBaseline = { doc, sinceBaseline: ChangeSet.empty(doc.length) };
    const line = doc.line(2);
    expect(baselineLineAt(baseline, line.from)).toBe(doc.lineAt(line.from).number);
  });
});
