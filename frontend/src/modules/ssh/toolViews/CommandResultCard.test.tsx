import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { isValidElement, type ReactElement, type ReactNode } from "react";
import { CommandResultCard, CommandResultView } from "./CommandResultCard";
import { commandDisplay } from "./parse";

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

  test("a multi-line command keeps its breaks", () => {
    const html = renderToString(<CommandResultCard result={{ command: "cd /srv &&\n  ls", output: "x" }} />);
    expect(html).toMatch(/<div class="[^"]*whitespace-pre-wrap[^"]*"><span[^>]*>\$/);
  });

  test("shows output without ANSI escapes or overdrawn progress", () => {
    const html = text(renderToString(<CommandResultCard result={{ command: "x", output: "\x1b[32mgreen\x1b[0m\n10%\r100%\n" }} />));
    expect(html).toContain("green");
    expect(html).not.toContain("[32m");
    expect(html).not.toContain("10%");
    expect(html).toContain(">2 lines<");
  });

  test("a command with no output says so", () => {
    const html = text(renderToString(<CommandResultCard result={{ command: "true", output: "" }} />));
    expect(html).toContain("No output");
    expect(html).toContain(">0 lines<");
  });
});

/** Every element in a tree of elements, props that hold elements included (a header's `action`). */
function elements(node: ReactNode, out: ReactElement[] = []): ReactElement[] {
  if (Array.isArray(node)) {
    for (const child of node) elements(child, out);
  } else if (isValidElement(node)) {
    out.push(node);
    for (const value of Object.values(node.props as Record<string, unknown>)) elements(value as ReactNode, out);
  }
  return out;
}

function hasText(node: ReactNode, text: string): boolean {
  if (typeof node === "string") return node === text;
  if (Array.isArray(node)) return node.some((child) => hasText(child, text));
  return false;
}

describe("Copy", () => {
  test("copies the whole raw output — escapes, carriage returns and the lines past 20", () => {
    const raw = `\x1b[1m${many}\x1b[0m\n10%\r100%\n`;
    const copied: string[] = [];
    const tree = CommandResultView({
      result: { command: "seq 30", output: raw },
      display: commandDisplay(raw, false),
      all: false,
      copied: false,
      onToggleAll: () => {},
      onCopy: (value) => copied.push(value),
    });
    const copy = elements(tree).find(
      (el) => typeof (el.props as { onClick?: unknown }).onClick === "function" && hasText((el.props as { children?: ReactNode }).children, "Copy"),
    );
    expect(copy).toBeDefined();
    (copy!.props as { onClick: () => void }).onClick();
    expect(copied).toEqual([raw]);
  });
});
