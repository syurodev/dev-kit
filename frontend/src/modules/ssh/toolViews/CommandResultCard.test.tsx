import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { CommandResultCard } from "./CommandResultCard";

const text = (html: string) => html.replace(/<!-- -->/g, "");
const many = Array.from({ length: 30 }, (_, i) => `row-${i + 1}`).join("\n");

describe("CommandResultCard", () => {
  test("is a Command card counting the output's lines", () => {
    const html = text(renderToString(<CommandResultCard result={{ command: "ls", output: "a\nb\nc\n" }} />));
    expect(html).toContain('data-testid="tool-result-card"');
    expect(html).toContain(">Command<");
    expect(html).toContain(">3 lines<");
  });

  test("shows the command in mono behind a $ prompt", () => {
    const html = text(renderToString(<CommandResultCard result={{ command: "df -h /", output: "x" }} />));
    expect(html).toContain("$");
    expect(html).toContain("df -h /");
    expect(html).toMatch(/text-code-12[^"]*"[^>]*>[^<]*<span[^>]*>\$/);
  });

  test("the output wraps anywhere in the code face", () => {
    const html = renderToString(<CommandResultCard result={{ command: "ls", output: "a\nb" }} />);
    expect(html).toMatch(/<pre[^>]*class="[^"]*whitespace-pre-wrap[^"]*break-all[^"]*text-code-12/);
  });

  test("long output shows 20 lines, with Show all", () => {
    const html = text(renderToString(<CommandResultCard result={{ command: "seq 30", output: many }} />));
    expect(html).toContain("row-20");
    expect(html).not.toContain("row-21");
    expect(html).toContain("Show all");
    expect(html).toContain("10 more lines");
  });

  test("short output has no Show all", () => {
    expect(renderToString(<CommandResultCard result={{ command: "ls", output: "a" }} />)).not.toContain("Show all");
  });

  test("offers Copy, and no exit status (the tool does not report one)", () => {
    const html = text(renderToString(<CommandResultCard result={{ command: "ls", output: "a" }} />));
    expect(html).toContain("Copy");
    expect(html).not.toMatch(/exit|status-success|status-danger/i);
  });

  test("a command with no output says so", () => {
    const html = text(renderToString(<CommandResultCard result={{ command: "true", output: "" }} />));
    expect(html).toContain("No output");
    expect(html).toContain(">0 lines<");
  });
});
