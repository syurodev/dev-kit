import { describe, expect, test } from "bun:test";
import { splitFrontMatter } from "./markdownFrontMatter";

describe("splitFrontMatter", () => {
  test("sets a leading block aside and keeps every byte of both parts", () => {
    const src = "---\ntitle: A\ntags: [x]\n---\n# Heading\n\ntext\n";
    const { front, body } = splitFrontMatter(src);
    expect(front).toBe("---\ntitle: A\ntags: [x]\n---\n");
    expect(body).toBe("# Heading\n\ntext\n");
    expect(front + body).toBe(src);
  });

  test("a rule further down, or a file with none, has no front matter", () => {
    expect(splitFrontMatter("# T\n\n---\n\nmore\n").front).toBe("");
    expect(splitFrontMatter("text\n---\nmore\n---\n").front).toBe("");
    expect(splitFrontMatter("").body).toBe("");
  });

  test("an unclosed opening rule is not front matter", () => {
    expect(splitFrontMatter("---\ntitle: A\n\nbody").front).toBe("");
  });

  test("a file that is only front matter leaves an empty body", () => {
    expect(splitFrontMatter("---\na: 1\n---")).toEqual({ front: "---\na: 1\n---", body: "" });
  });
});
