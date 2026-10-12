import { describe, expect, it } from "bun:test";

import { continueList, formatShortcut, nextMarker, readListMarker, toggleList, toggleWrap } from "./listEditing";

const apply = (text: string, edit: { from: number; to: number; text: string }) =>
  text.slice(0, edit.from) + edit.text + text.slice(edit.to);

describe("readListMarker", () => {
  it("reads bullets and numbers, with their indent", () => {
    expect(readListMarker("- one")).toMatchObject({ kind: "bullet", text: "-", length: 2 });
    expect(readListMarker("  * two")).toMatchObject({ kind: "bullet", indent: "  ", length: 4 });
    expect(readListMarker("3) three")).toMatchObject({ kind: "ordered", number: 3, text: "3)" });
  });
  it("is not fooled by text that only looks like one", () => {
    for (const line of ["-x", "1.5 litres", "2024-01-01", "**bold**", "", "-", "1."]) {
      expect(readListMarker(line)).toBeNull();
    }
  });
});

describe("continueList", () => {
  it("carries the bullet or counts on", () => {
    expect(continueList("- one", 5)).toEqual({ kind: "continue", insert: "\n- " });
    expect(continueList("  9. nine", 9)).toEqual({ kind: "continue", insert: "\n  10. " });
    expect(nextMarker(readListMarker("2) x")!)).toBe("3)");
  });
  it("ends the list on an empty item", () => {
    expect(continueList("- ", 2)).toEqual({ kind: "end", from: 0, to: 2 });
    expect(continueList("1.   ", 5)).toEqual({ kind: "end", from: 0, to: 5 });
  });
  it("leaves an ordinary line, or a caret inside the marker, alone", () => {
    expect(continueList("plain", 5)).toBeNull();
    expect(continueList("- one", 1)).toBeNull();
  });
});

describe("toggleList", () => {
  it("makes the touched lines a list and numbers an ordered one", () => {
    const text = "intro\nalpha\nbeta\noutro";
    const edit = toggleList(text, text.indexOf("alpha"), text.indexOf("beta") + 2, "ordered");
    expect(apply(text, edit)).toBe("intro\n1. alpha\n2. beta\noutro");
  });
  it("takes the markers off when every line already has them", () => {
    const text = "- a\n- b";
    expect(apply(text, toggleList(text, 0, text.length, "bullet"))).toBe("a\nb");
  });
  it("switches kind rather than stacking markers", () => {
    const text = "- a\n- b";
    expect(apply(text, toggleList(text, 0, text.length, "ordered"))).toBe("1. a\n2. b");
  });
  it("starts a list on an empty line, and keeps indentation and blank lines", () => {
    const empty = toggleList("", 0, 0, "bullet");
    expect(apply("", empty)).toBe("- ");
    expect(empty.select).toEqual({ start: 2, end: 2 });
    const text = "  a\n\n  b";
    expect(apply(text, toggleList(text, 0, text.length, "bullet"))).toBe("  - a\n\n  - b");
  });
});

describe("toggleWrap", () => {
  it("wraps a selection and unwraps it again", () => {
    const wrapped = toggleWrap("say hello now", 4, 9, "**");
    expect(apply("say hello now", wrapped)).toBe("say **hello** now");
    expect(wrapped.select).toEqual({ start: 6, end: 11 });
    const text = "say **hello** now";
    expect(apply(text, toggleWrap(text, 6, 11, "**"))).toBe("say hello now");
    expect(apply(text, toggleWrap(text, 4, 13, "**"))).toBe("say hello now");
  });
  it("puts the caret between the marks when nothing is selected", () => {
    const edit = toggleWrap("ab", 1, 1, "*");
    expect(apply("ab", edit)).toBe("a**b");
    expect(edit.select).toEqual({ start: 2, end: 2 });
  });
  it("does not take the edge of bold for italic", () => {
    const text = "**hi**";
    expect(apply(text, toggleWrap(text, 2, 4, "*"))).toBe("***hi***");
  });
});

describe("formatShortcut", () => {
  const key = (init: Partial<Parameters<typeof formatShortcut>[0]>) =>
    formatShortcut({ key: "", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false, ...init });
  it("reads the four shortcuts and nothing else", () => {
    expect(key({ key: "b" })).toEqual({ kind: "wrap", mark: "**" });
    expect(key({ key: "I", metaKey: false, ctrlKey: true })).toEqual({ kind: "wrap", mark: "*" });
    expect(key({ key: "*", code: "Digit8", shiftKey: true })).toEqual({ kind: "list", list: "bullet" });
    expect(key({ key: "&", code: "Digit7", shiftKey: true })).toEqual({ kind: "list", list: "ordered" });
    expect(key({ key: "b", metaKey: false })).toBeNull();
    expect(key({ key: "b", altKey: true })).toBeNull();
    expect(key({ key: "c" })).toBeNull();
  });
});
