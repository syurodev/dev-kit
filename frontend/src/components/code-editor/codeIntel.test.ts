import { describe, expect, it } from "bun:test";
import { ChangeSet, Text } from "@codemirror/state";
import {
  baselineLineAt,
  buildCodeIntelMenu,
  currentLineAt,
  firstSelectableIndex,
  formatLocationPath,
  hasCodeIntelMenu,
  identifierAt,
  lastSelectableIndex,
  nextSelectableIndex,
  pickerHeading,
  shortcutHint,
  splitPreviewSegments,
  type CodeIntelBaseline,
} from "./codeIntel";

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

describe("currentLineAt", () => {
  it("is baselineLineAt's inverse: maps a baseline line forward to where it now sits", () => {
    const baselineDoc = Text.of(["first", "second", "target", "fourth"]);
    // Same edit as baselineLineAt's own test, read the other way: two lines
    // inserted above everything moves "target" from baseline line 3 to
    // current line 5.
    const sinceBaseline = ChangeSet.of({ from: 0, insert: "one\ntwo\n" }, baselineDoc.length);
    const baseline: CodeIntelBaseline = { doc: baselineDoc, sinceBaseline };
    const currentDoc = sinceBaseline.apply(baselineDoc);

    expect(currentLineAt(baseline, 3, currentDoc)).toBe(5);
  });

  it("equals the baseline line right after a baseline reset", () => {
    const doc = Text.of(["alpha", "beta", "gamma"]);
    const baseline: CodeIntelBaseline = { doc, sinceBaseline: ChangeSet.empty(doc.length) };
    expect(currentLineAt(baseline, 2, doc)).toBe(2);
  });

  it("tracks a line that moved DOWN when text was deleted above it", () => {
    const baselineDoc = Text.of(["one", "two", "three", "target"]);
    // Deletes "one\n" — everything after it, including "target", shifts up by
    // one line instead of down; the mapping must follow either direction.
    const sinceBaseline = ChangeSet.of({ from: 0, to: 4, insert: "" }, baselineDoc.length);
    const baseline: CodeIntelBaseline = { doc: baselineDoc, sinceBaseline };
    const currentDoc = sinceBaseline.apply(baselineDoc);

    expect(currentLineAt(baseline, 4, currentDoc)).toBe(3);
  });

  it("clamps a baseline line beyond the baseline document rather than throwing", () => {
    const doc = Text.of(["only line"]);
    const baseline: CodeIntelBaseline = { doc, sinceBaseline: ChangeSet.empty(doc.length) };
    expect(currentLineAt(baseline, 99, doc)).toBe(1);
  });
});

describe("splitPreviewSegments", () => {
  it("emphasises whole-identifier occurrences only", () => {
    expect(splitPreviewSegments("list.toResponse()", "toResponse")).toEqual([
      { text: "list.", match: false },
      { text: "toResponse", match: true },
      { text: "()", match: false },
    ]);
  });

  it("does not match an identifier that is only a substring of a longer one", () => {
    expect(splitPreviewSegments("return toResponseWrapper(x);", "toResponse")).toEqual([
      { text: "return toResponseWrapper(x);", match: false },
    ]);
  });

  it("emphasises every occurrence", () => {
    expect(splitPreviewSegments("find(id) or find(other)", "find")).toEqual([
      { text: "find", match: true },
      { text: "(id) or ", match: false },
      { text: "find", match: true },
      { text: "(other)", match: false },
    ]);
  });

  it("returns the whole preview unmatched when the identifier never occurs", () => {
    expect(splitPreviewSegments("no match here", "toResponse")).toEqual([{ text: "no match here", match: false }]);
  });

  it("returns nothing for an empty preview", () => {
    expect(splitPreviewSegments("", "toResponse")).toEqual([]);
  });
});

describe("formatLocationPath", () => {
  it("names the editor's own file as 'this file'", () => {
    expect(formatLocationPath({ path: "src/a.ts", line: 5 }, "src/a.ts")).toEqual({
      text: "this file:5",
      isCurrentFile: true,
    });
  });

  it("shows any other file's path as-is", () => {
    expect(formatLocationPath({ path: "src/b.ts", line: 9 }, "src/a.ts")).toEqual({
      text: "src/b.ts:9",
      isCurrentFile: false,
    });
  });
});

describe("pickerHeading", () => {
  it("pluralises definitions and uses 'of'", () => {
    expect(pickerHeading("definition", "findAll", 2)).toBe("2 definitions of findAll");
  });

  it("pluralises implementations and uses 'of'", () => {
    expect(pickerHeading("implementation", "list", 3)).toBe("3 implementations of list");
  });

  it("uses the singular 'reference' and 'to' for a single reference", () => {
    expect(pickerHeading("references", "single", 1)).toBe("1 reference to single");
  });

  it("pluralises references and uses 'to'", () => {
    expect(pickerHeading("references", "toResponse", 12)).toBe("12 references to toResponse");
  });
});

describe("shortcutHint", () => {
  it("renders mac glyphs with no separator, in Option/Shift/Command order", () => {
    expect(shortcutHint("F12", "mac")).toBe("F12");
    expect(shortcutHint("Mod-F12", "mac")).toBe("⌘F12");
    expect(shortcutHint("Mod-Alt-F12", "mac")).toBe("⌥⌘F12");
    expect(shortcutHint("Shift-F12", "mac")).toBe("⇧F12");
    expect(shortcutHint("Mod-X", "mac")).toBe("⌘X");
    expect(shortcutHint("Mod-C", "mac")).toBe("⌘C");
  });

  it("renders Ctrl/Alt/Shift with '+' everywhere else", () => {
    for (const platform of ["windows", "linux", "web"] as const) {
      expect(shortcutHint("F12", platform)).toBe("F12");
      expect(shortcutHint("Mod-F12", platform)).toBe("Ctrl+F12");
      expect(shortcutHint("Mod-Alt-F12", platform)).toBe("Ctrl+Alt+F12");
      expect(shortcutHint("Shift-F12", platform)).toBe("Shift+F12");
      expect(shortcutHint("Mod-X", platform)).toBe("Ctrl+X");
      expect(shortcutHint("Mod-C", platform)).toBe("Ctrl+C");
    }
  });
});

describe("hasCodeIntelMenu", () => {
  it("is false when the provider has no capability at all", () => {
    expect(hasCodeIntelMenu({ definition: false, implementation: false, references: false })).toBe(false);
  });

  it("is true when at least one capability is present", () => {
    expect(hasCodeIntelMenu({ definition: false, implementation: false, references: true })).toBe(true);
  });
});

describe("buildCodeIntelMenu", () => {
  const ALL = { definition: true, implementation: true, references: true };

  it("lists every lookup row, a separator, then Cut and Copy", () => {
    const rows = buildCodeIntelMenu(ALL, false, "mac");
    expect(rows.map((row) => (row.kind === "separator" ? "—" : row.action))).toEqual([
      "definition",
      "definitionBeside",
      "implementation",
      "references",
      "—",
      "cut",
      "copy",
    ]);
  });

  it("drops the definition rows and the implementations row for a references-only provider", () => {
    const rows = buildCodeIntelMenu({ definition: false, implementation: false, references: true }, false, "mac");
    expect(rows.map((row) => (row.kind === "separator" ? "—" : row.action))).toEqual(["references", "—", "cut", "copy"]);
  });

  it("drops Cut on a read-only buffer, keeping Copy", () => {
    const rows = buildCodeIntelMenu(ALL, true, "mac");
    expect(rows.some((row) => row.kind === "item" && row.action === "cut")).toBe(false);
    expect(rows.some((row) => row.kind === "item" && row.action === "copy")).toBe(true);
  });

  it("never inserts a separator when there are no lookup rows", () => {
    const rows = buildCodeIntelMenu({ definition: false, implementation: false, references: false }, false, "mac");
    expect(rows.every((row) => row.kind !== "separator")).toBe(true);
    expect(rows.map((row) => (row.kind === "item" ? row.action : "—"))).toEqual(["cut", "copy"]);
  });

  it("renders platform-appropriate shortcuts", () => {
    const rows = buildCodeIntelMenu(ALL, false, "windows");
    const definition = rows.find((row) => row.kind === "item" && row.action === "definition");
    expect(definition && definition.kind === "item" && definition.shortcut).toBe("F12");
    const beside = rows.find((row) => row.kind === "item" && row.action === "definitionBeside");
    expect(beside && beside.kind === "item" && beside.shortcut).toBe("Ctrl+Alt+F12");
  });
});

describe("nextSelectableIndex / firstSelectableIndex / lastSelectableIndex", () => {
  const allSelectable = () => true;

  it("moves forward and wraps", () => {
    expect(nextSelectableIndex(3, 0, 1, allSelectable)).toBe(1);
    expect(nextSelectableIndex(3, 2, 1, allSelectable)).toBe(0);
  });

  it("moves backward and wraps", () => {
    expect(nextSelectableIndex(3, 0, -1, allSelectable)).toBe(2);
  });

  it("skips indices a menu's separators/disabled rows reject", () => {
    // index 1 is a separator, not selectable.
    const isSelectable = (index: number) => index !== 1;
    expect(nextSelectableIndex(3, 0, 1, isSelectable)).toBe(2);
    expect(nextSelectableIndex(3, 2, -1, isSelectable)).toBe(0);
  });

  it("stays put when nothing else qualifies", () => {
    const isSelectable = (index: number) => index === 0;
    expect(nextSelectableIndex(3, 0, 1, isSelectable)).toBe(0);
  });

  it("returns -1 for an empty row set", () => {
    expect(nextSelectableIndex(0, 0, 1, allSelectable)).toBe(-1);
    expect(firstSelectableIndex(0, allSelectable)).toBe(-1);
    expect(lastSelectableIndex(0, allSelectable)).toBe(-1);
  });

  it("finds the first and last selectable index, skipping what's rejected", () => {
    const isSelectable = (index: number) => index !== 0 && index !== 4;
    expect(firstSelectableIndex(5, isSelectable)).toBe(1);
    expect(lastSelectableIndex(5, isSelectable)).toBe(3);
  });
});
