import { describe, expect, it } from "bun:test";
import { detectEol } from "./eol";

describe("detectEol", () => {
  it("keeps a pure-LF or pure-CRLF file byte for byte", () => {
    expect(detectEol("a\nb\n")).toEqual({ text: "a\nb\n", eol: "\n", mixed: false });
    expect(detectEol("a\r\nb\r\n")).toEqual({ text: "a\r\nb\r\n", eol: "\r\n", mixed: false });
    expect(detectEol("no newline")).toEqual({ text: "no newline", eol: "\n", mixed: false });
    expect(detectEol("")).toEqual({ text: "", eol: "\n", mixed: false });
  });

  it("normalizes a mixed file to its dominant ending and says so", () => {
    expect(detectEol("a\r\nb\r\nc\n")).toEqual({ text: "a\r\nb\r\nc\r\n", eol: "\r\n", mixed: true });
    expect(detectEol("a\nb\nc\r\n")).toEqual({ text: "a\nb\nc\n", eol: "\n", mixed: true });
  });

  it("treats a bare CR as mixed", () => {
    const r = detectEol("a\rb\n");
    expect(r.mixed).toBe(true);
    expect(r.text.includes("\r") && !r.text.includes("\r\n")).toBe(false);
  });
});
