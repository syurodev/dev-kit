import { describe, expect, test } from "bun:test";
import { chordLabel, parseGlyphShortcut, platformLabel } from "./shortcut";

describe("chordLabel", () => {
  test("writes modifiers ⌘ ⌥ ⇧ and the key on macOS, as the Keyboard settings draws them", () => {
    expect(chordLabel({ mod: true, alt: true, key: "1" }, true)).toBe("⌘⌥1");
    expect(chordLabel({ mod: true, shift: true, key: "." }, true)).toBe("⌘⇧.");
    expect(chordLabel({ mod: true, shift: true, key: "Enter" }, true)).toBe("⌘⇧↵");
    expect(chordLabel({ mod: true, key: "k" }, true)).toBe("⌘K");
  });

  test("spells them out elsewhere", () => {
    expect(chordLabel({ mod: true, alt: true, key: "1" }, false)).toBe("Ctrl+Alt+1");
    expect(chordLabel({ mod: true, shift: true, key: "Enter" }, false)).toBe("Ctrl+Shift+Enter");
    expect(chordLabel({ mod: true, key: "," }, false)).toBe("Ctrl+,");
  });
});

describe("parseGlyphShortcut", () => {
  test("reads a manifest's glyph shortcut into a chord", () => {
    expect(parseGlyphShortcut("⇧⌘N")).toEqual({ mod: true, shift: true, key: "N" });
    expect(parseGlyphShortcut("⌘↵")).toEqual({ mod: true, key: "Enter" });
    expect(parseGlyphShortcut("⌘.")).toEqual({ mod: true, key: "." });
  });

  test("gives up on what it has no chord for", () => {
    expect(parseGlyphShortcut("⌘")).toBeNull();
    expect(parseGlyphShortcut("⌃1")).toBeNull();
    expect(parseGlyphShortcut("⌘AB")).toBeNull();
  });
});

describe("platformLabel", () => {
  test("leaves a macOS label as its author wrote it", () => {
    expect(platformLabel("⇧⌘N", true)).toBe("⇧⌘N");
    expect(platformLabel("⌘↵", true)).toBe("⌘↵");
  });

  test("spells a glyph chord out elsewhere", () => {
    expect(platformLabel("⇧⌘N", false)).toBe("Ctrl+Shift+N");
    expect(platformLabel("⌘↵", false)).toBe("Ctrl+Enter");
    expect(platformLabel("⌘,", false)).toBe("Ctrl+,");
    expect(platformLabel("⌫", false)).toBe("Backspace");
  });

  test("returns what is not a chord as written", () => {
    expect(platformLabel("esc", false)).toBe("esc");
    expect(platformLabel("Double-click", false)).toBe("Double-click");
    expect(platformLabel("⌃⇧1", false)).toBe("⌃⇧1");
  });
});
