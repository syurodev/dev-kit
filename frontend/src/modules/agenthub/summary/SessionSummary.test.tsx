import { describe, expect, test } from "bun:test";
import type * as React from "react";
import { renderToString } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { EMPTY_CHECKPOINTS, type CheckpointIndex } from "../checkpoints";
import type { TranscriptItem } from "../types";
import { SessionSummaryBody, SessionSummaryButton, SessionSummaryCard } from "./SessionSummary";

function render(node: React.ReactNode, documents?: { id: string; name: string; pages: number; size: number }[]) {
  const client = new QueryClient();
  if (documents) client.setQueryData(["agenthub", "documents", "s"], documents);
  return renderToString(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

const planItem = {
  id: "p",
  kind: "plan",
  plan: [
    { content: "Read the code", status: "completed" },
    { content: "Write the fix", status: "in_progress" },
    { content: "Run the tests" },
  ],
} as TranscriptItem;

function checkpointsWith(paths: string[]): CheckpointIndex {
  return new Map([
    [
      "p1",
      {
        id: "c1",
        createdAt: "2026-10-13T10:00:00Z",
        status: "done",
        changed: paths.map((path) => ({ path, kind: "modified", added: 2, removed: 1, binary: false, source: "agent" })),
      },
    ],
  ]) as never;
}

const base = { sessionId: "s", items: [] as TranscriptItem[], checkpoints: EMPTY_CHECKPOINTS, usage: null, turnCount: 0, live: false };

describe("SessionSummaryBody", () => {
  test("renders only the sections that have something", () => {
    const html = render(<SessionSummaryBody {...base} items={[planItem]} />);
    expect(html).toContain('data-testid="summary-plan"');
    expect(html).not.toContain('data-testid="summary-changes"');
    expect(html).not.toContain('data-testid="summary-sources"');
    expect(html).not.toContain('data-testid="summary-subagents"');
    expect(html).not.toContain('data-testid="summary-context"');
  });

  test("plan rows say done / now / pending", () => {
    const html = render(<SessionSummaryBody {...base} items={[planItem]} />);
    const states = [...html.matchAll(/data-testid="summary-plan-row" data-state="(\w+)"/g)].map((m) => m[1]);
    expect(states).toEqual(["completed", "in_progress", "pending"]);
    // The header counts what is done out of everything.
    expect(html).toContain("1/3");
  });

  test("a section shows five then 'See all (N)'", () => {
    const html = render(
      <SessionSummaryBody {...base} checkpoints={checkpointsWith(["a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go"])} />,
    );
    expect(html.match(/data-testid="summary-change-row"/g)?.length).toBe(5);
    expect(html).toContain('data-testid="summary-see-all"');
    expect(html).toContain("See all (7)");
  });

  test("five or fewer have no 'See all'", () => {
    const html = render(<SessionSummaryBody {...base} checkpoints={checkpointsWith(["a.go", "b.go"])} />);
    expect(html.match(/data-testid="summary-change-row"/g)?.length).toBe(2);
    expect(html).not.toContain('data-testid="summary-see-all"');
  });

  test("a change row carries its line counts and the full path", () => {
    const html = render(<SessionSummaryBody {...base} checkpoints={checkpointsWith(["internal/agenthub/service.go"])} />);
    expect(html).toContain('title="internal/agenthub/service.go"');
    expect(html).toContain("+2");
    expect(html).toContain("−1");
  });

  test("sources list PDFs, DevKit tools and link hosts", () => {
    const items = [
      { id: "1", kind: "tool_call", toolCall: { name: "mcp__devkit__db_query" } },
      { id: "2", kind: "tool_call", toolCall: { name: "db.explain" } },
      { id: "3", kind: "user_message_chunk", text: "see https://example.com/x" },
    ] as TranscriptItem[];
    const html = render(<SessionSummaryBody {...base} items={items} />, [{ id: "doc1", name: "spec.pdf", pages: 12, size: 1 }]);
    expect(html).toContain('data-testid="summary-sources"');
    expect(html).toContain("spec.pdf");
    expect(html).toContain("12 pages");
    expect(html).toContain("Database");
    expect(html).toContain("2 calls");
    expect(html).toContain("example.com");
  });

  test("context shows the meter and the numbers when the window size is known", () => {
    const html = render(<SessionSummaryBody {...base} usage={{ used: 42, size: 100, amount: 0.18 }} turnCount={12} />);
    expect(html).toContain('data-testid="summary-context"');
    expect(html).toContain("42% · 12 turns · $0.18");
  });

  test("nothing at all renders nothing", () => {
    expect(render(<SessionSummaryBody {...base} />)).toBe("");
  });
});

describe("the wrappers", () => {
  test("the docked card is a labelled section", () => {
    const html = render(<SessionSummaryCard {...base} items={[planItem]} />);
    expect(html).toContain('aria-label="Session summary"');
    expect(html).toContain('data-testid="session-summary-card"');
    expect(html).toContain('data-testid="summary-plan"');
  });

  test("nothing at all renders nothing", () => {
    expect(render(<SessionSummaryCard {...base} />)).toBe("");
    expect(render(<SessionSummaryButton {...base} />)).toBe("");
  });

  test("the collapsed button is labelled and holds the body in a closed popover", () => {
    const html = render(<SessionSummaryButton {...base} items={[planItem]} />);
    expect(html).toContain('data-testid="session-summary-button"');
    expect(html).toContain('aria-label="Session summary"');
    // Closed: the sections are not drawn until it opens.
    expect(html).not.toContain('data-testid="summary-plan"');
  });
});
