import { describe, expect, it } from "bun:test";
import { firstNonWhitespaceColumn } from "./CodeEditor";

describe("firstNonWhitespaceColumn", () => {
  it("finds the first non-whitespace character", () => {
    expect(firstNonWhitespaceColumn("    return repository.find(id);")).toBe(4);
  });

  it("returns 0 for a line with no leading whitespace", () => {
    expect(firstNonWhitespaceColumn("class Foo {")).toBe(0);
  });

  it("returns 0 for a blank line", () => {
    expect(firstNonWhitespaceColumn("")).toBe(0);
  });

  it("returns 0 for a line that is only whitespace", () => {
    expect(firstNonWhitespaceColumn("      ")).toBe(0);
  });

  it("counts a tab as one column, same as CodeMirror's own column model", () => {
    expect(firstNonWhitespaceColumn("\t\tfoo();")).toBe(2);
  });
});
