import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { hasToolResultView, renderToolResult } from "./toolResultViews";
import { parseMemoryEntry, parseMemoryPage, registerMemoryResultViews } from "./memoryResultViews";
import { MemoryEntryCard, MemoryListCard } from "./MemoryResultCard";

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

  it("a deleted entry says so", () => {
    const html = renderToString(<MemoryEntryCard entry={parseMemoryEntry({ ...ENTRY, deleted: true })!} now={NOW} />);
    expect(html).toContain("Deleted");
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
  it("get/create/update and list/search have views; a shape they cannot read falls back", () => {
    registerMemoryResultViews();
    for (const tool of ["get", "create", "update", "list", "search"]) {
      expect(hasToolResultView(`workspace.memory.${tool}`)).toBe(true);
      expect(hasToolResultView(`mcp__devkit__workspace_memory_${tool}`)).toBe(true);
    }
    expect(renderToolResult({ name: "workspace.memory.get", rawInput: {}, rawOutput: ENTRY })).not.toBeNull();
    expect(renderToolResult({ name: "workspace.memory.get", rawInput: {}, rawOutput: { entries: [] } })).toBeNull();
    expect(renderToolResult({ name: "workspace.memory.list", rawInput: {}, rawOutput: ENTRY })).toBeNull();
  });
});
