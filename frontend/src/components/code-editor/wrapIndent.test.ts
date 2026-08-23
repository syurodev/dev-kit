import { describe, expect, it } from "bun:test";
import { leadingIndentWidth } from "./wrapIndent";

describe("leadingIndentWidth", () => {
  it("counts leading spaces", () => {
    expect(leadingIndentWidth("        @AuthenticationPrincipal Jwt jwt,")).toBe(8);
  });

  it("is zero for a line starting at the margin", () => {
    expect(leadingIndentWidth("public class Foo {")).toBe(0);
  });

  it("expands a tab to the next tab stop, not a flat width", () => {
    expect(leadingIndentWidth("\tx", 4)).toBe(4);
    expect(leadingIndentWidth("  \tx", 4)).toBe(4); // 2 spaces, then the tab fills to 4
    expect(leadingIndentWidth("\t\tx", 4)).toBe(8);
  });

  it("stops at the first non-whitespace character", () => {
    expect(leadingIndentWidth("  var x = '   spaced';")).toBe(2);
  });

  it("handles whitespace-only and empty lines", () => {
    expect(leadingIndentWidth("    ")).toBe(4);
    expect(leadingIndentWidth("")).toBe(0);
  });
});
