import { describe, expect, test } from "bun:test";
import { isPdfPath, viewerKindOf } from "./viewerKind";

describe("viewerKindOf", () => {
  test("reads the kind off the name, case-insensitively", () => {
    expect(viewerKindOf("docs/guide.md")).toBe("markdown");
    expect(viewerKindOf("README.MARKDOWN")).toBe("markdown");
    expect(viewerKindOf("a/b/Spec.PDF")).toBe("pdf");
    expect(viewerKindOf("main.go")).toBe("text");
  });

  test("an extension in the middle of a name does not count", () => {
    expect(viewerKindOf("notes.md.bak")).toBe("text");
    expect(viewerKindOf("pdf")).toBe("text");
    expect(viewerKindOf("report.pdf.txt")).toBe("text");
  });

  test("mdx stays text: it is JSX, not markdown", () => {
    expect(viewerKindOf("docs/page.mdx")).toBe("text");
  });

  test("only a pdf has no buffer", () => {
    expect(isPdfPath("a.pdf")).toBe(true);
    expect(isPdfPath("a.md")).toBe(false);
  });
});
