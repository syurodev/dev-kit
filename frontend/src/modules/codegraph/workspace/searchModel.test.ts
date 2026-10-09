import { describe, expect, it } from "bun:test";
import type { SearchResult } from "@/bridge/workspaceCode";
import { buildRows, fileGroupKey, leadingContext, folderGroupKey, previewParts, requestSignature, resultSummary, splitPatterns, DEFAULT_SEARCH_OPTIONS } from "./searchModel";

const m = (line: number, column = 1, preview = "const x = needle;", previewStart = 0, length = 6) => ({ line, column, length, preview, previewStart });
const result = (over: Partial<SearchResult> = {}): SearchResult => ({
  files: [
    { folderId: "a", path: "src/one.ts", matches: [m(1, 11), m(7, 11)], truncated: false },
    { folderId: "a", path: "src/two.ts", matches: [m(3, 11)], truncated: false },
    { folderId: "c", path: "src/one.ts", matches: [m(2, 11)], truncated: false },
  ],
  folders: [
    { folderId: "a", state: "ok", error: "", source: "git", files: 10, duplicateOf: "" },
    { folderId: "b", state: "ok", error: "", source: "walk", files: 4, duplicateOf: "" },
    { folderId: "c", state: "ok", error: "", source: "git", files: 2, duplicateOf: "" },
    { folderId: "d", state: "unavailable", error: "folder not found", source: "", files: 0, duplicateOf: "" },
  ],
  matches: 4,
  filesSearched: 16,
  filesSkipped: 0,
  truncated: "",
  elapsedMs: 5,
  ...over,
});

describe("buildRows", () => {
  it("groups folder → file → matches, and keeps same-named files in different folders apart", () => {
    const rows = buildRows(result(), []);
    expect(rows.map((r) => r.kind === "match" ? "m" : r.kind === "file" ? `file:${r.folderId}:${r.path}` : `folder:${r.folderId}`)).toEqual([
      "folder:a", "file:a:src/one.ts", "m", "m", "file:a:src/two.ts", "m",
      "folder:c", "file:c:src/one.ts", "m",
      "folder:d",
    ]);
    const keys = rows.map((r) => r.key);
    expect(new Set(keys).size).toBe(keys.length);
  });

  it("lists a folder with no hits only when something is wrong with it", () => {
    const rows = buildRows(result(), []);
    expect(rows.some((r) => r.kind === "folder" && r.folderId === "b")).toBe(false);
    const d = rows.find((r) => r.kind === "folder" && r.folderId === "d");
    expect(d && d.kind === "folder" && d.state).toBe("unavailable");
    expect(buildRows(result(), [], { showEmptyFolders: true }).some((r) => r.kind === "folder" && r.folderId === "b")).toBe(true);
  });

  it("folded groups hide their children", () => {
    const rows = buildRows(result(), [folderGroupKey("a"), fileGroupKey("c", "src/one.ts")]);
    expect(rows.filter((r) => r.kind === "match")).toHaveLength(0);
    expect(rows.some((r) => r.kind === "file" && r.folderId === "a")).toBe(false);
    expect(rows.some((r) => r.kind === "file" && r.folderId === "c")).toBe(true);
  });
});

describe("previewParts", () => {
  it("highlights by the preview's own offset, not the line's", () => {
    // The line's column 300 is offset 299; the preview begins at 279, so the hit is at 20 of it.
    const parts = previewParts({ line: 1, column: 300, length: 6, preview: "…aaaaaaaaaaaaaaaaaaaneedle bbb", previewStart: 279 });
    expect(parts.hit).toBe("needle");
    expect(parts.before.endsWith("a")).toBe(true);
  });

  it("clamps rather than throwing on a stale offset", () => {
    expect(previewParts({ line: 1, column: 99, length: 5, preview: "abc", previewStart: 0 })).toEqual({ before: "abc", hit: "", after: "" });
  });
});

describe("requests", () => {
  it("splits patterns on commas and drops blanks", () => {
    expect(splitPatterns(" *.ts, src/** ,, ")).toEqual(["*.ts", "src/**"]);
    expect(splitPatterns("")).toEqual([]);
  });

  it("a result is for one workspace, one scope and one set of options", () => {
    const base = requestSignature("w", ["a"], { ...DEFAULT_SEARCH_OPTIONS, query: "x" });
    expect(requestSignature("w", ["a"], { ...DEFAULT_SEARCH_OPTIONS, query: "x" })).toBe(base);
    expect(requestSignature("w2", ["a"], { ...DEFAULT_SEARCH_OPTIONS, query: "x" })).not.toBe(base);
    expect(requestSignature("w", ["a", "b"], { ...DEFAULT_SEARCH_OPTIONS, query: "x" })).not.toBe(base);
    expect(requestSignature("w", ["a"], { ...DEFAULT_SEARCH_OPTIONS, query: "x", wholeWord: true })).not.toBe(base);
    expect(requestSignature("w", ["a"], { ...DEFAULT_SEARCH_OPTIONS, query: "x", exclude: "dist" })).not.toBe(base);
  });

  it("says plainly when the answer is cut", () => {
    expect(resultSummary(result())).toContain("4 matches in 3 files");
    expect(resultSummary(result({ truncated: "matches" }))).toContain("first matches only");
    expect(resultSummary(result({ filesSkipped: 3 }))).toContain("3 skipped");
  });
});

describe("leadingContext", () => {
  it("keeps a short lead as it is", () => {
    expect(leadingContext("  const x = ")).toBe("const x = ");
  });
  it("cuts a long lead from the left so the hit is visible in a narrow row", () => {
    const lead = leadingContext("a".repeat(200_000));
    expect(lead.length).toBe(15);
    expect(lead.startsWith("…")).toBe(true);
  });
  it("does not split an emoji", () => {
    const lead = leadingContext("x".repeat(20) + "😀".repeat(7), 14);
    expect(lead.slice(1)).not.toMatch(/^[\udc00-\udfff]/);
  });
});

describe("resultSummary", () => {
  it("says when a file was cut, since its count is not its total", () => {
    const r = result({ files: [{ folderId: "a", path: "big.txt", matches: [m(1)], truncated: true }], matches: 1 });
    expect(resultSummary(r)).toContain("1 file cut at 500 matches");
  });
});
