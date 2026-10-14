import { describe, expect, test } from "bun:test";
import * as React from "react";
import { renderToString as renderRaw } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentProse } from "./AgentMessageRich";
import { TranscriptActionsProvider } from "./transcriptActions";
import { FilePreviewView, highlightedLines, previewSlice, ReferencePreview, UrlPreview } from "./ReferencePreview";
import type { RefTarget } from "./resolveFileRef";

const renderToString = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);
const noop = () => {};

const FILE = Array.from({ length: 20 }, (_, i) => `line ${i + 1}`).join("\n") + "\n";
const T: RefTarget = { workspaceId: "ws", folderId: "api", folderName: "api", rel: "internal/auth/session.go", line: 12 };

describe("previewSlice", () => {
  test("four lines either side of the target", () => {
    const slice = previewSlice(FILE, 12);
    expect(slice.startLine).toBe(8);
    expect(slice.lines).toEqual(["line 8", "line 9", "line 10", "line 11", "line 12", "line 13", "line 14", "line 15", "line 16"]);
  });

  test("clamped to the start and the end of the file", () => {
    expect(previewSlice(FILE, 2)).toEqual({ startLine: 1, lines: ["line 1", "line 2", "line 3", "line 4", "line 5", "line 6"] });
    const end = previewSlice(FILE, 19);
    expect(end.startLine).toBe(15);
    // The trailing newline is not a 21st line.
    expect(end.lines[end.lines.length - 1]).toBe("line 20");
  });

  test("a line past the end shows the end", () => {
    const past = previewSlice(FILE, 99).lines;
    expect(past[past.length - 1]).toBe("line 20");
  });

  test("with no line, the first nine lines", () => {
    const slice = previewSlice(FILE, undefined);
    expect(slice.startLine).toBe(1);
    expect(slice.lines).toHaveLength(9);
    expect(slice.lines[0]).toBe("line 1");
    expect(previewSlice("a\nb", undefined)).toEqual({ startLine: 1, lines: ["a", "b"] });
  });
});

describe("highlightedLines", () => {
  test("plain until highlighted, one node per line", () => {
    expect(highlightedLines("a\nb", null)).toEqual(["a", "b"]);
  });

  test("a syntax run that spans lines is split at the newline", () => {
    const lines = highlightedLines("x = /*a\nb*/", {
      code: "x = /*a\nb*/",
      segments: [
        { text: "x = ", className: "" },
        { text: "/*a\nb*/", className: "tok-comment" },
      ],
    });
    expect(lines).toHaveLength(2);
    const html = lines.map((line) => renderRaw(<>{line}</>));
    expect(html[0]).toBe('x = <span class="tok-comment">/*a</span>');
    expect(html[1]).toBe('<span class="tok-comment">b*/</span>');
  });
});

describe("FilePreviewView", () => {
  const view = (state: React.ComponentProps<typeof FilePreviewView>["state"]) =>
    renderToString(<FilePreviewView fileRef={{ path: "auth/session.go", line: 12 }} state={state} onOpen={noop} onPick={noop} />);

  test("text: header, numbered lines, the target line raised, Open and Open here", () => {
    const html = view({ kind: "text", target: T, content: FILE });
    expect(html).toContain(">session.go<");
    expect(html).toContain("internal/auth/");
    expect(html).toContain(">api<");
    expect(html.match(/data-testid="ref-preview-line"/g)?.length).toBe(9);
    expect(html.match(/data-target="true"/g)?.length).toBe(1);
    expect(html).toContain("bg-state-soft");
    expect(html).toContain("tabular-nums");
    expect(html).toContain(">Open<");
    expect(html).toContain(">Open here<");
  });

  test("loading draws three placeholder lines", () => {
    expect(view({ kind: "loading" }).match(/data-slot="skeleton"/g)?.length).toBe(3);
  });

  test("unreadable says why", () => {
    expect(view({ kind: "unreadable", target: T, reason: "binary" })).toContain("Binary file");
    expect(view({ kind: "unreadable", target: T, reason: "too_large" })).toContain("Too large to preview");
  });

  test("missing says so", () => {
    expect(view({ kind: "missing" })).toContain("Not found in this workspace");
  });

  test("ambiguous lists the candidates", () => {
    const B = { ...T, folderId: "gw", folderName: "gateway" };
    expect(view({ kind: "ambiguous", targets: [T, B] }).match(/data-testid="agent-ref-pick"/g)?.length).toBe(2);
  });

  test("unsupported offers Code Graph only", () => {
    const html = view({ kind: "unsupported" });
    expect(html).toContain("Open in Code Graph");
    expect(html).not.toContain(">Open here<");
  });
});

describe("UrlPreview", () => {
  test("the whole URL, its host, open and copy", () => {
    const html = renderToString(<UrlPreview url="https://grafana.local/d/auth?x=1" />);
    expect(html).toContain("https://grafana.local/d/auth?x=1");
    expect(html).toContain("break-all");
    expect(html).toContain(">grafana.local<");
    expect(html).toContain("Open in browser");
    expect(html).toContain("Copy link");
  });
});

describe("ReferencePreview", () => {
  test("closed, the body is not rendered", () => {
    const html = renderToString(
      <ReferencePreview preview={() => <p data-testid="body">body</p>} menu={null}>
        <span>trigger</span>
      </ReferencePreview>,
    );
    expect(html).toContain("trigger");
    expect(html).not.toContain('data-testid="body"');
  });
});

describe("AgentProse references", () => {
  const prose = (text: string) =>
    renderToString(
      <TranscriptActionsProvider value={{ openRef: noop }}>
        <AgentProse text={text} />
      </TranscriptActionsProvider>,
    );
  // The hover card trigger's slot, and the context menu trigger's touch-callout style, on one element.
  const wrapped = (html: string, testid: string) =>
    new RegExp(`<[a-z]+(?=[^>]*data-testid="${testid}")(?=[^>]*data-slot="hover-card-trigger")(?=[^>]*-webkit-touch-callout:none)`).test(html);

  test("a file ref sits in a hover card and a context menu", () => {
    const html = prose("Look at `auth/session.go:88`");
    expect(wrapped(html, "agent-file-ref")).toBe(true);
    expect(html).toContain('role="button"');
  });

  test("a URL link and a file link do too", () => {
    expect(wrapped(prose("[web](https://a.dev/x)"), "agent-link")).toBe(true);
    expect(wrapped(prose("[file](src/a.ts#L3)"), "agent-link")).toBe(true);
  });

  test("a link DevKit will not open is plain text: no role, no tab stop, no menu", () => {
    const html = prose("[ftp](ftp://host/f)");
    expect(html).toContain('data-link="other"');
    expect(html).not.toContain("role=");
    expect(html).not.toContain("tabindex");
    expect(html).not.toContain("hover-card-trigger");
    expect(html).not.toContain("<a ");
    expect(html).not.toContain("underline");
  });
});
