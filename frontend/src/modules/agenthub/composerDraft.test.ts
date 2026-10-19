import { describe, expect, test } from "bun:test";
import type { AgentImageAttachment } from "@/bridge/agenthub";
import { encodeEntityRef } from "@/workbench/entityRef";
import {
  appendChips,
  chipToken,
  draftHasContent,
  fileChip,
  imageChip,
  parseDraft,
  refChip,
  scrubText,
  splitDraft,
  turnFromDraft,
} from "./composerDraft";

const image = (id: string, name = "shot.png"): AgentImageAttachment => ({
  id,
  name,
  mimeType: "image/png",
  data: "AAAA",
  previewUrl: "data:image/png;base64,AAAA",
  size: 3,
});

describe("a draft with chips", () => {
  test("plain text is its own agent text and has no chips", () => {
    expect(parseDraft("hello\nworld")).toEqual({ text: "hello\nworld", chips: [] });
  });

  test("a file chip reads as @name to the agent and keeps its path", () => {
    const draft = `compare ${chipToken(fileChip("/home/me/a b.md"))} please`;
    expect(parseDraft(draft)).toEqual({
      text: "compare @a b.md please",
      chips: [{ kind: "file", key: "/home/me/a b.md", label: "a b.md" }],
    });
  });

  test("a path or label with the token's own punctuation survives", () => {
    const chip = { kind: "file" as const, key: "/x/odd|name:1 (final).pdf", label: "odd|name:1 (final).pdf" };
    expect(parseDraft(`> ${chipToken(chip)}`).chips).toEqual([chip]);
  });

  test("splitDraft gives the text and the chips in order", () => {
    const parts = splitDraft(`a${chipToken(fileChip("/p/b.txt"))}c`);
    expect(parts.map((part) => (part.type === "text" ? part.text : part.chip.label))).toEqual(["a", "b.txt", "c"]);
  });

  test("a stray private-use character in typed text cannot make a chip", () => {
    const typed = scrubText("afile:%2Fetc%2Fpasswd|xb");
    expect(parseDraft(typed).chips).toEqual([]);
    expect(typed).not.toContain("");
  });

  test("a half token is text, not a crash", () => {
    expect(parseDraft("file:/x").chips).toEqual([]);
  });

  test("content: words, or any chip", () => {
    expect(draftHasContent("")).toBe(false);
    expect(draftHasContent("  \n ")).toBe(false);
    expect(draftHasContent("hi")).toBe(true);
    expect(draftHasContent(chipToken(fileChip("/a.txt")))).toBe(true);
  });
});

describe("turnFromDraft", () => {
  const ref = { module: "codegraph", kind: "endpoint", id: "GET /users", label: "GET /users" };

  test("reads what a turn carries off the draft, in order and once each", () => {
    const draft = [
      "look at",
      chipToken(fileChip("/p/a.md")),
      chipToken(imageChip(image("i1"))),
      chipToken(fileChip("/p/a.md")),
      chipToken(refChip(ref)),
      chipToken(fileChip("/p/b.md")),
    ].join(" ");
    const turn = turnFromDraft(draft, { i1: image("i1") });
    expect(turn.attachments).toEqual(["/p/a.md", "/p/b.md"]);
    expect(turn.images.map((item) => item.id)).toEqual(["i1"]);
    expect(turn.refs).toEqual([{ ...ref, project: undefined, line: undefined }]);
    expect(turn.text).toStartWith("look at @a.md @shot.png @a.md @GET /users @b.md");
  });

  test("an image whose bytes are gone is dropped, the rest still go", () => {
    const draft = `${chipToken(imageChip(image("gone")))} ${chipToken(fileChip("/p/a.md"))}`;
    const turn = turnFromDraft(draft, {});
    expect(turn.images).toEqual([]);
    expect(turn.attachments).toEqual(["/p/a.md"]);
  });

  test("the text is trimmed, as a typed prompt was", () => {
    expect(turnFromDraft("  hi \n", {}).text).toBe("hi");
  });
});

describe("appendChips", () => {
  test("puts chips after the text with a space between, and one after", () => {
    const next = appendChips("look at", [fileChip("/p/a.md")]);
    expect(next).toBe(`look at ${chipToken(fileChip("/p/a.md"))} `);
    expect(parseDraft(next).text).toBe("look at @a.md ");
  });

  test("into an empty draft there is no leading space", () => {
    expect(appendChips("", [fileChip("/p/a.md"), fileChip("/p/b.md")])).toBe(
      `${chipToken(fileChip("/p/a.md"))} ${chipToken(fileChip("/p/b.md"))} `,
    );
  });

  test("a draft that already ends in whitespace gets no second one", () => {
    expect(appendChips("look at\n", [fileChip("/p/a.md")])).toBe(`look at\n${chipToken(fileChip("/p/a.md"))} `);
  });

  test("a ref chip round trips through its URI", () => {
    const ref = { module: "database", kind: "table", id: "public.users", label: "users", project: "/work/app", line: 4 };
    const chip = refChip(ref);
    expect(chip.key).toBe(encodeEntityRef(ref));
    expect(turnFromDraft(appendChips("", [chip]), {}).refs).toEqual([ref]);
  });
});
