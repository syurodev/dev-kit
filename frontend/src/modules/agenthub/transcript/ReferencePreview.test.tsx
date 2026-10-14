import { describe, expect, test } from "bun:test";
import * as React from "react";
import { renderToString as renderRaw } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentProse } from "./AgentMessageRich";
import { TranscriptActionsProvider } from "./transcriptActions";
import { HIGHLIGHT_LIMIT } from "@/components/code/highlight";
import type { EntityRef, IntentOptions } from "@/workbench/intents";
import type { FileRef } from "./fileRef";
import {
  FilePreviewView,
  FileReference,
  highlightedLines,
  hoverMayOpen,
  previewRows,
  previewSlice,
  previewWindow,
  ReferencePreview,
  UrlPreview,
  UrlReference,
  useFileRefOpener,
} from "./ReferencePreview";
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

describe("previewSlice line endings", () => {
  test("CRLF files split into the same lines", () => {
    expect(previewSlice("a\r\nb\r\n", 1)).toEqual({ startLine: 1, lines: ["a", "b"] });
  });
});

describe("previewWindow", () => {
  test("a file the highlighter takes is highlighted whole, once", () => {
    const w = previewWindow(FILE, 12);
    expect(w.codeStart).toBe(1);
    expect(w.code).toBe(FILE.replace(/\n$/, ""));
    expect([w.startLine, w.endLine, w.target, w.total]).toEqual([8, 16, 12, 20]);
  });

  test("a line past the end is the last line", () => {
    const w = previewWindow(FILE, 99);
    expect([w.startLine, w.endLine, w.target, w.total]).toEqual([16, 20, 20, 20]);
  });

  test("CRLF is read as LF", () => {
    expect(previewWindow("a\r\nb", 1).code).toBe("a\nb");
  });

  test("past the highlight limit, two hundred lines either side of the target", () => {
    const big = Array.from({ length: 4000 }, (_, i) => `const value${i} = ${i}; // padding`).join("\n");
    expect(big.length).toBeGreaterThan(HIGHLIGHT_LIMIT);
    const w = previewWindow(big, 2000);
    expect(w.codeStart).toBe(1800);
    expect(w.code.split("\n")).toHaveLength(401);
    expect(w.code.startsWith("const value1799 ")).toBe(true);
  });
});

describe("previewRows", () => {
  test("a target inside a block comment that opens above the slice keeps the comment's colour", () => {
    const lines = ["package a", "/*", ...Array.from({ length: 22 }, (_, i) => `comment ${i + 3}`), "*/", "func b() {}"];
    const content = lines.join("\n");
    const w = previewWindow(content, 20);
    const open = content.indexOf("/*");
    const close = content.indexOf("*/") + 2;
    const rows = previewRows(w, {
      code: w.code,
      segments: [
        { text: content.slice(0, open), className: "" },
        { text: content.slice(open, close), className: "tok-comment" },
        { text: content.slice(close), className: "" },
      ],
    });
    expect(rows).toHaveLength(9);
    expect(renderRaw(<>{rows[4]}</>)).toBe('<span class="tok-comment">comment 20</span>');
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

  test("loading draws the final nine rows, under the path as written", () => {
    const html = view({ kind: "loading" });
    expect(html.match(/data-slot="skeleton"/g)?.length).toBe(9);
    expect(html).toContain(">session.go<");
    expect(html).toContain("auth/");
  });

  test("every state keeps the same body box and the footer row", () => {
    const B = { ...T, folderId: "gw", folderName: "gateway" };
    const states: React.ComponentProps<typeof FilePreviewView>["state"][] = [
      { kind: "loading" },
      { kind: "text", target: T, content: FILE },
      { kind: "unreadable", target: T, reason: "binary" },
      { kind: "missing" },
      { kind: "ambiguous", targets: [T, B] },
      { kind: "unsupported" },
      { kind: "error", message: "boom" },
    ];
    for (const state of states) {
      const html = view(state);
      expect(html.match(/data-testid="ref-preview-body"/g)?.length).toBe(1);
      // The box is sized by nine invisible rows, whatever fills it.
      expect(html.match(/data-testid="ref-preview-sizer"/g)?.length).toBe(1);
      expect(html.match(/data-testid="ref-preview-footer"/g)?.length).toBe(1);
    }
  });

  test("a line past the end raises the last line and says how long the file is", () => {
    const html = renderToString(
      <FilePreviewView fileRef={{ path: "a.go", line: 99 }} state={{ kind: "text", target: { ...T, line: 99 }, content: FILE }} onOpen={noop} onPick={noop} />,
    );
    expect(html).toContain("line 99 of 20");
    expect(html).toMatch(/data-target="true"[^>]*>line 20</);
  });

  test("the code keeps tabs narrow and a long file name truncates", () => {
    const html = view({ kind: "text", target: T, content: FILE });
    expect(html).toContain("tab-size-2");
    expect(html).toMatch(/class="[^"]*min-w-0[^"]*truncate[^"]*"[^>]*>session\.go</);
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
    expect(html).toContain("No workspace to resolve this in.");
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

describe("hoverMayOpen", () => {
  test("only a pointer resting on the reference opens it", () => {
    expect(hoverMayOpen({ pointerInside: true, pressed: false, menuOpen: false })).toBe(true);
    // Keyboard focus alone: Enter opens, the context menu offers the rest.
    expect(hoverMayOpen({ pointerInside: false, pressed: false, menuOpen: false })).toBe(false);
    // A click opened the file; the card stays shut until the pointer leaves.
    expect(hoverMayOpen({ pointerInside: true, pressed: true, menuOpen: false })).toBe(false);
    expect(hoverMayOpen({ pointerInside: true, pressed: false, menuOpen: true })).toBe(false);
  });
});

describe("useFileRefOpener", () => {
  test("the card's Open and a picked candidate use the surface's placement", async () => {
    const opened: { ref: FileRef; placement?: string }[] = [];
    const dispatched: { ref: EntityRef; options: IntentOptions }[] = [];
    let opener: ReturnType<typeof useFileRefOpener> | null = null;
    function Probe() {
      opener = useFileRefOpener({ path: "a.go", line: 3 }, async (_name, ref, options = {}) => {
        dispatched.push({ ref, options });
        return "ok";
      });
      return null;
    }
    renderRaw(
      <TranscriptActionsProvider value={{ refPlacement: "replace", openRef: (ref, _anchor, placement) => opened.push({ ref, placement }) }}>
        <Probe />
      </TranscriptActionsProvider>,
    );
    opener!.open(null);
    expect(opened).toEqual([{ ref: { path: "a.go", line: 3 }, placement: undefined }]);
    await opener!.pick(T);
    expect(dispatched[0].options.placement).toBe("replace");
    expect(dispatched[0].ref.line).toBe(3);
  });
});

describe("ReferencePreview", () => {
  test("the trigger says it has a menu", () => {
    const html = renderToString(
      <ReferencePreview preview={() => null} menu={null}>
        <span>trigger</span>
      </ReferencePreview>,
    );
    expect(html).toContain('aria-haspopup="menu"');
  });

  test("file and URL references are memoised on their props, not the element", () => {
    const memo = Symbol.for("react.memo");
    expect((FileReference as unknown as { $$typeof: symbol }).$$typeof).toBe(memo);
    expect((UrlReference as unknown as { $$typeof: symbol }).$$typeof).toBe(memo);
  });

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
