import { describe, expect, test } from "bun:test";
import { KEYS } from "platejs";
import { createPlateEditor } from "platejs/react";
import { NotesEditorKit } from "./plugins";

/** A note is stored as Markdown: what is written must come back unchanged. */
function roundTrip(markdown: string) {
  const editor = createPlateEditor({ plugins: NotesEditorKit });
  const value = editor.api.markdown.deserialize(markdown);
  editor.children = value;
  return { value, markdown: editor.api.markdown.serialize().trim() };
}

describe("callouts", () => {
  test("a GitHub alert is a callout of that type, and writes back the same", () => {
    const source = "> [!NOTE]\n> Useful **info**";
    const { value, markdown } = roundTrip(source);
    expect(value).toHaveLength(1);
    expect(value[0]).toMatchObject({ type: KEYS.callout, variant: "note" });
    expect(markdown).toBe(source);
  });

  for (const variant of ["NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"]) {
    test(`${variant} survives`, () => {
      const source = `> [!${variant}]\n> text`;
      const { value, markdown } = roundTrip(source);
      expect(value[0]).toMatchObject({ type: KEYS.callout, variant: variant.toLowerCase() });
      expect(markdown).toBe(source);
    });
  }

  test("the marker is read in any case, and written in capitals", () => {
    const { value, markdown } = roundTrip("> [!tip]\n> lower");
    expect(value[0]).toMatchObject({ type: KEYS.callout, variant: "tip" });
    expect(markdown).toBe("> [!TIP]\n> lower");
  });

  test("several blocks, a list among them, keep their shape", () => {
    const source = "> [!WARNING]\n> First paragraph\n>\n> * one\n> * two\n>\n> Last";
    const { value, markdown } = roundTrip(source);
    expect(value[0]).toMatchObject({ type: KEYS.callout, variant: "warning" });
    expect((value[0] as { children: unknown[] }).children.length).toBeGreaterThan(2);
    expect(markdown).toBe(source);
  });

  test("a code block inside a callout survives", () => {
    const source = "> [!TIP]\n> Run this:\n>\n> ```sh\n> bun test\n> ```";
    expect(roundTrip(source).markdown).toBe(source);
  });

  test("a marker with nothing after it is an empty callout, and stays one", () => {
    const { value, markdown } = roundTrip("> [!NOTE]");
    expect(value[0]).toMatchObject({ type: KEYS.callout, variant: "note" });
    expect(markdown).toBe("> [!NOTE]");
  });

  test("text on the marker's own line is kept as the first paragraph", () => {
    const { value } = roundTrip("> [!NOTE] Heads up\n> more");
    expect(value[0]).toMatchObject({ type: KEYS.callout, variant: "note" });
    expect(JSON.stringify(value[0])).toContain("Heads up");
  });

  test("an ordinary quote is still a quote", () => {
    const { value, markdown } = roundTrip("> just a quote");
    expect(value[0]).toMatchObject({ type: KEYS.blockquote });
    expect(markdown).toBe("> just a quote");
  });

  test("an alert type GitHub does not have stays a quote", () => {
    const { value } = roundTrip("> [!INFO]\n> text");
    expect(value[0]).toMatchObject({ type: KEYS.blockquote });
  });
});

describe("table of contents", () => {
  test("[TOC] is a table of contents, and writes back as [TOC]", () => {
    const { value, markdown } = roundTrip("# Title\n\n[TOC]\n\n## Section");
    expect(value.map((node) => node.type)).toEqual([KEYS.h1, KEYS.toc, KEYS.h2]);
    expect(markdown).toBe("# Title\n\n[TOC]\n\n## Section");
  });

  test("the marker is read in any case", () => {
    expect(roundTrip("[toc]").value[0]).toMatchObject({ type: KEYS.toc });
  });

  test("[TOC] inside a sentence is text", () => {
    const { value } = roundTrip("See the [TOC] above");
    expect(value[0]).toMatchObject({ type: KEYS.p });
  });
});

describe("fenced diagrams and equations", () => {
  test("math and mermaid fences keep their language and their lines", () => {
    for (const source of ["```math\nE = mc^2\n\\int_0^1 x\\,dx\n```", "```mermaid\nflowchart LR\n  A --> B\n```"]) {
      const { value, markdown } = roundTrip(source);
      expect(value[0]).toMatchObject({ type: KEYS.codeBlock });
      expect(markdown).toBe(source);
    }
  });
});
