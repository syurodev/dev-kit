import { describe, expect, it } from "bun:test";
import { PREVIEW_FOLDED_CHARS, previewPermissionText } from "./permissionPreviewText";

describe("previewPermissionText", () => {
  it("indents valid JSON by two spaces and keeps the folded text as given", () => {
    const raw = '{"profileId":"p1","sql":"select 1"}';
    const out = previewPermissionText(raw);
    expect(out.json).toBe(true);
    expect(out.collapsed).toBe(raw);
    expect(out.expanded).toBe('{\n  "profileId": "p1",\n  "sql": "select 1"\n}');
    expect(out.copy).toBe(out.expanded);
    expect(out.long).toBe(false);
  });

  it("nests objects and arrays and writes empty ones inline", () => {
    const out = previewPermissionText('{"a":{"b":[1,{"c":null}],"d":{}},"e":[]}');
    expect(out.expanded).toBe(
      ['{', '  "a": {', '    "b": [', '      1,', '      {', '        "c": null', '      }', '    ],', '    "d": {}', '  },', '  "e": []', '}'].join("\n"),
    );
  });

  it("shows line breaks inside a string as real ones, only in the expanded text", () => {
    const raw = JSON.stringify({ message: "one\ntwo\n\nthree" });
    const out = previewPermissionText(raw);
    expect(out.expanded).toBe('{\n  "message": "one\ntwo\n\nthree"\n}');
    expect(out.collapsed).toBe(raw);
    // Copy stays valid JSON: the exact input, laid out.
    expect(JSON.parse(out.copy)).toEqual({ message: "one\ntwo\n\nthree" });
    expect(out.copy).toContain("one\\ntwo");
    expect(out.long).toBe(true);
  });

  it("keeps quotes, backslashes and control characters escaped", () => {
    const value = 'say "hi" \\ done\t!';
    const out = previewPermissionText(JSON.stringify({ q: value }));
    expect(out.expanded).toBe(`{\n  "q": ${JSON.stringify(value)}\n}`);
  });

  it("treats text that is not JSON as plain text", () => {
    for (const raw of ["ls -la && echo done", "{not json", "[1, 2", "42", '"str"']) {
      const out = previewPermissionText(raw);
      expect(out.json).toBe(false);
      expect(out.expanded).toBe(raw);
      expect(out.copy).toBe(raw);
    }
  });

  it("offers more only when the folded box would cut something", () => {
    expect(previewPermissionText("echo hi").long).toBe(false);
    expect(previewPermissionText("x".repeat(PREVIEW_FOLDED_CHARS + 1)).long).toBe(true);
    expect(previewPermissionText("line one\nline two").long).toBe(true);
  });

  it("keeps a very long single string whole", () => {
    const text = "word ".repeat(2000);
    const out = previewPermissionText(JSON.stringify({ message: text }));
    expect(out.long).toBe(true);
    expect(out.expanded).toBe(`{\n  "message": ${JSON.stringify(text)}\n}`);
    expect(out.expanded.length).toBeGreaterThan(10_000);
  });

  it("leaves Vietnamese and other unicode text as it is, not as \\u escapes", () => {
    const message = "Bạn là session con của 8d2fcfa7. Nhiệm vụ dễ, CHỈ ĐỌC / PHÂN TÍCH 🙂\nTiếp theo";
    const out = previewPermissionText(JSON.stringify({ message }));
    expect(out.expanded).toContain("Bạn là session con");
    expect(out.expanded).toContain("CHỈ ĐỌC / PHÂN TÍCH 🙂\nTiếp theo");
    expect(out.expanded).not.toContain("\\u");
    expect(JSON.parse(out.copy).message).toBe(message);
  });
});
