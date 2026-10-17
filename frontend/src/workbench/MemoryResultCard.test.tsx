import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { hasToolResultView, renderToolResult } from "./toolResultViews";
import { parseMemoryEntry, parseMemoryPage } from "./memoryResultViews";
import { MemoryEntryCard, MemoryListCard, overflowsClamp, previewParagraphs } from "./MemoryResultCard";

// workspace.memory.* as internal/workspacememory answers (camelCase JSON).
const ENTRY = {
  id: "m1",
  workspaceId: "w1",
  kind: "decision",
  title: "Use cursor pagination",
  summary: "Offsets drift when rows are inserted.",
  body: "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\nLine 6\nLine 7\nLine 8",
  revision: 3,
  deleted: false,
  createdAt: "2026-10-14T08:00:00Z",
  updatedAt: "2026-10-14T10:00:00Z",
  source: { caller: "agent", sessionId: "s1", agentType: "claude" },
};
const NOW = Date.parse("2026-10-14T12:00:00Z");

const summary = (id: string, kind: string, title: string, text: string) => {
  const { body: _body, ...rest } = ENTRY;
  return { ...rest, id, kind, title, summary: text };
};

describe("workspace.memory entry", () => {
  it("reads an Entry and declines anything else", () => {
    expect(parseMemoryEntry(ENTRY)?.title).toBe("Use cursor pagination");
    expect(parseMemoryEntry({ id: "m1" })).toBeNull();
    expect(parseMemoryEntry("text")).toBeNull();
  });

  it("a write answers without the body: the card shows the one the call sent", () => {
    // What create/update return now (internal/mcp/tools_workspace_memory.go memoryEntryOut).
    const { body, workspaceId: _w, createdAt: _c, source: _s, deleted: _d, ...compact } = ENTRY;
    expect(parseMemoryEntry(compact, { body })?.body).toBe(body);
    expect(parseMemoryEntry(compact, { body })?.revision).toBe(3);
    // An answer that carries the body (includeBody, or get) wins over the input's.
    expect(parseMemoryEntry({ ...compact, body: "from answer" }, { body })?.body).toBe("from answer");
    // A delete or restore sent no body: a card without one, not a JSON fallback.
    expect(parseMemoryEntry(compact, { id: "m1", expectedRevision: 2 })?.body).toBe("");
  });

  it("kind chip, title, summary, body clamped to 6 lines with Show all, revision and age", () => {
    const html = renderToString(<MemoryEntryCard entry={parseMemoryEntry(ENTRY)!} now={NOW} />);
    expect(html).toContain("decision");
    expect(html).toContain("Use cursor pagination");
    expect(html).toContain("Offsets drift when rows are inserted.");
    expect(html).toContain("line-clamp-6");
    expect(html).toContain("Line 8");
    expect(html).toContain("Show all");
    expect(html).toContain("revision 3 · updated 2h ago");
    expect(html).not.toContain("Deleted");
  });

  it("a short body has nothing to show all of", () => {
    const html = renderToString(<MemoryEntryCard entry={parseMemoryEntry({ ...ENTRY, body: "One line" })!} now={NOW} />);
    expect(html).not.toContain("Show all");
  });

  it("a deleted entry says so, as a state rather than a danger", () => {
    const html = renderToString(<MemoryEntryCard entry={parseMemoryEntry({ ...ENTRY, deleted: true })!} now={NOW} />);
    expect(html).toContain("Deleted");
    expect(html).toContain("line-through");
    expect(html).not.toContain("status-danger");
  });

  it("Show all names the body it opens", () => {
    const html = renderToString(<MemoryEntryCard entry={parseMemoryEntry(ENTRY)!} now={NOW} />);
    const controls = /aria-controls="([^"]+)"/.exec(html)?.[1];
    expect(controls).toBeTruthy();
    expect(html).toContain(`id="${controls}"`);
  });

  // A blank line is a line to line-clamp: one at the cut left "…" alone on it.
  it("the folded body's paragraphs drop blank lines, so the cut lands on text", () => {
    expect(previewParagraphs("A\n\nB\nC\n\n\n\nD\n\n  \n")).toEqual(["A", "B\nC", "D"]);
    expect(previewParagraphs("\n\nA  \n \t\nB")).toEqual(["A", "B"]);
    expect(previewParagraphs("only\nlines")).toEqual(["only\nlines"]);
    expect(previewParagraphs("\n  indented\nnext\r\n\r\nlast")).toEqual(["  indented\nnext", "last"]);
  });

  it("folded, the body is its paragraphs with no blank line between them", () => {
    const body = "Context.\n\nDecision.\nA refund.\n\n\nAfter.\n\n";
    const folded = renderToString(<MemoryEntryCard entry={parseMemoryEntry({ ...ENTRY, body })!} now={NOW} />);
    const box = folded.slice(folded.indexOf("line-clamp-6"));
    expect(box).toContain(">Context.</p>");
    expect(box).toContain(">Decision.\nA refund.</p>");
    expect(box).toContain(">After.</p>");
    expect(box).not.toContain("\n\n");
  });

  it("measures the clamp: overflowing only when the text runs past the box", () => {
    expect(overflowsClamp({ scrollHeight: 200, clientHeight: 120 })).toBe(true);
    expect(overflowsClamp({ scrollHeight: 120, clientHeight: 120 })).toBe(false);
    expect(overflowsClamp({ scrollHeight: 121, clientHeight: 120 })).toBe(false);
  });
});

describe("workspace.memory list / search", () => {
  const PAGE = {
    entries: [
      summary("m1", "convention", "Branch names", "feat/<area>-<topic>"),
      summary("m2", "handoff", "Sync rewrite", "Halfway through the pull path"),
    ],
    hasMore: true,
    nextCursor: "c2",
  };

  it("reads a page and declines anything else", () => {
    expect(parseMemoryPage(PAGE)?.entries).toHaveLength(2);
    expect(parseMemoryPage({ entries: "x" })).toBeNull();
    expect(parseMemoryPage(ENTRY)).toBeNull();
  });

  it("rows [kind] title — summary, and More results available", () => {
    const html = renderToString(<MemoryListCard page={parseMemoryPage(PAGE)!} />);
    expect(html).toContain("[convention]");
    expect(html).toContain("Branch names");
    expect(html).toContain("— feat/&lt;area&gt;-&lt;topic&gt;");
    expect(html).toContain("[handoff]");
    expect(html).toContain("More results available");
  });

  it("the last page says nothing more", () => {
    const html = renderToString(<MemoryListCard page={parseMemoryPage({ ...PAGE, hasMore: false })!} />);
    expect(html).not.toContain("More results available");
  });
});

describe("registration", () => {
  it("the workbench's registration point registers every memory view, and can undo it", async () => {
    const { workbenchViews } = await import("./registerViews");
    for (const tool of ["get", "create", "update", "delete", "restore", "list", "search"]) {
      expect(hasToolResultView(`workspace.memory.${tool}`)).toBe(true);
      expect(hasToolResultView(`mcp__devkit__workspace_memory_${tool}`)).toBe(true);
    }
    expect(renderToolResult({ name: "workspace.memory.get", rawInput: {}, rawOutput: ENTRY })).not.toBeNull();
    expect(renderToolResult({ name: "workspace.memory.get", rawInput: {}, rawOutput: { entries: [] } })).toBeNull();
    expect(renderToolResult({ name: "workspace.memory.list", rawInput: {}, rawOutput: ENTRY })).toBeNull();
    expect(renderToolResult({ name: "workspace.memory.restore", rawInput: {}, rawOutput: ENTRY })).not.toBeNull();
    // A compact write (no body in the answer) is still the entry card, not the JSON fallback.
    const { body, workspaceId: _w, createdAt: _c, source: _s, deleted: _d, ...compact } = ENTRY;
    for (const tool of ["create", "update"]) {
      expect(renderToolResult({ name: `workspace.memory.${tool}`, rawInput: { body }, rawOutput: compact })).not.toBeNull();
    }
    expect(workbenchViews).toHaveLength(7);
    for (const unregister of workbenchViews) unregister();
    expect(hasToolResultView("workspace.memory.delete")).toBe(false);
  });
});
