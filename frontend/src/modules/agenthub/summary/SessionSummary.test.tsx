import { describe, expect, test } from "bun:test";
import type * as React from "react";
import { renderToString } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { EMPTY_CHECKPOINTS, type CheckpointIndex } from "../checkpoints";
import type { Connector, SessionGoal, TranscriptItem, Usage } from "../types";
import type { SummaryRow } from "./summaryModel";
import {
  SessionSummaryBody as Body,
  SessionSummaryButton as Button,
  SessionSummaryCard as Card,
  documentAttachmentCount,
  summarySubagents,
  useSessionSummaryData,
  type SessionSummaryInput,
} from "./SessionSummary";

type Props = SessionSummaryInput & {
  usage: Usage | null;
  turnCount: number;
  environment?: readonly SummaryRow[];
  goal?: SessionGoal | null;
  connectors?: readonly Connector[];
};

// The view takes the derived data; these derive it the way AgentHubContent does.
function SessionSummaryBody(props: Props) {
  return <Body {...props} data={useSessionSummaryData(props)} />;
}
function SessionSummaryCard(props: Props) {
  return <Card {...props} data={useSessionSummaryData(props)} />;
}
function SessionSummaryButton(props: Props) {
  return <Button {...props} data={useSessionSummaryData(props)} />;
}

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
      { id: "2", kind: "tool_call", toolCall: { name: "db.query" } },
      { id: "2b", kind: "tool_call", toolCall: { name: "mcp__devkit__ssh_listProfiles" } },
      { id: "3", kind: "user_message_chunk", text: "see https://example.com/x" },
    ] as TranscriptItem[];
    const html = render(<SessionSummaryBody {...base} items={items} />, [{ id: "doc1", name: "spec.pdf", pages: 12, size: 1 }]);
    expect(html).toContain('data-testid="summary-sources"');
    expect(html).toContain("spec.pdf");
    expect(html).toContain("12 pages");
    expect(html).toContain("Database");
    expect(html).toContain("2 queries");
    expect(html).not.toContain("SSH");
    expect(html).not.toContain("call");
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

  test("a long plan's visible rows include the step in progress", () => {
    const plan = Array.from({ length: 8 }, (_, i) => ({
      content: `Step ${i}`,
      status: i < 6 ? "completed" : i === 6 ? "in_progress" : "pending",
    }));
    const html = render(<SessionSummaryBody {...base} items={[{ id: "p", kind: "plan", plan } as TranscriptItem]} />);
    expect(html.match(/data-testid="summary-plan-row"/g)?.length).toBe(5);
    expect(html).toContain('data-state="in_progress"');
    expect(html).toContain("See all (8)");
  });

  test("a binary change is not a button: it has no diff to open", () => {
    const index = new Map([
      [
        "p1",
        {
          id: "c1",
          createdAt: "2026-10-13T10:00:00Z",
          status: "done",
          changed: [{ path: "logo.png", kind: "modified", added: 0, removed: 0, binary: true, source: "agent" }],
        },
      ],
    ]) as never;
    const html = render(<SessionSummaryBody {...base} checkpoints={index} />);
    expect(html).toContain('<div data-testid="summary-change-row"');
    expect(html).toContain("binary");
  });

  test("finished subagents are listed after the running ones", () => {
    const tracked = [{ id: "a", name: "Done one", state: "completed" }, { id: "b", name: "Running one" }];
    const subagents = summarySubagents([], tracked, false);
    expect(subagents.map((a) => a.id)).toEqual(["b", "a"]);
    const data = { plan: [], changes: [], tools: [], links: [], docs: [], subagents };
    const html = render(<Body {...base} data={data} />);
    expect(html).toContain('data-testid="summary-subagents"');
    expect(html.indexOf("Running one")).toBeLessThan(html.indexOf("Done one"));
  });
});

describe("documentAttachmentCount", () => {
  test("counts the PDFs attached to the session's own prompts", () => {
    const items = [
      { id: "1", kind: "user_message_chunk", attachments: [{ name: "a.pdf", kind: "document" }, { name: "b.png", kind: "image" }] },
      { id: "2", kind: "user_message_chunk", attachments: [{ name: "c.pdf", kind: "document" }] },
      { id: "3", kind: "agent_message_chunk" },
    ] as TranscriptItem[];
    expect(documentAttachmentCount(items)).toBe(2);
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

// What a vendor reports decides what its summary has. Nothing here is drawn
// for a vendor that has no data for it: not empty, not a placeholder.
describe("vendor-driven sections", () => {
  const tokens = {
    session: { total: 1_000_000, input: 980_000, cached: 490_000, output: 20_000, reasoning: 5_000 },
    lastRequest: { total: 30_000, input: 29_000, cached: 0, output: 1_000, reasoning: 0 },
  };

  test("a Codex-shaped session shows its token split, plan windows and MCP servers", () => {
    const html = render(
      <SessionSummaryBody
        {...base}
        usage={{
          used: 30_000,
          size: 400_000,
          amount: 0,
          tokens,
          limits: [{ label: "5-hour", usedPercent: 42, status: "ok" }],
        }}
        connectors={[{ name: "devkit", state: "ready" }, { name: "github", state: "failed", needsLogin: true }]}
      />,
    );
    expect(html).toContain('data-testid="summary-token-row"');
    expect(html).toContain("980k · 50% cached");
    expect(html).toContain('data-testid="summary-limit-row"');
    expect(html).toContain("42%");
    expect(html).toContain('data-testid="summary-connectors"');
    expect(html).toContain("Sign-in needed");
    // Codex has no background tasks, and the section is not there to say so.
    expect(html).not.toContain('data-testid="summary-background"');
  });

  test("a Claude-shaped session shows background tasks and cost, and none of Codex's", () => {
    const html = render(
      <SessionSummaryBody
        {...base}
        items={[
          { id: "t1", kind: "async_task_spawned", asyncTask: { id: "t1", name: "Run the tests", state: "running", usage: { totalTokens: 10, toolUses: 3, durationMs: 72_000 } } } as TranscriptItem,
        ]}
        usage={{ used: 42, size: 100, amount: 0.18, limits: [{ label: "Weekly · Opus", usedPercent: 83, status: "warning" }] }}
      />,
    );
    expect(html).toContain('data-testid="summary-background"');
    expect(html).toContain("Run the tests");
    expect(html).toContain("$0.18");
    expect(html).toContain("Weekly · Opus");
    expect(html).not.toContain('data-testid="summary-token-row"');
    expect(html).not.toContain('data-testid="summary-connectors"');
  });

  test("plan windows alone draw the section even before the context window is known", () => {
    const html = render(
      <SessionSummaryBody {...base} usage={{ used: 0, size: 0, amount: 0, limits: [{ label: "5-hour", usedPercent: 10 }] }} />,
    );
    expect(html).toContain('data-testid="summary-context"');
    expect(html).toContain('data-testid="summary-limit-row"');
    expect(html).not.toContain('role="meter"');
  });

  test("environment and goal are drawn when given, and the changes carry their total", () => {
    const html = render(
      <SessionSummaryBody
        {...base}
        checkpoints={checkpointsWith(["a.go", "b.go"])}
        environment={[
          { key: "agent", label: "Agent", value: "Codex" },
          { key: "branch", label: "Branch", value: "feat/x" },
        ]}
        goal={{ objective: "Ship the viewer" }}
      />,
    );
    expect(html).toContain('data-testid="summary-environment"');
    expect(html).toContain("feat/x");
    expect(html).toContain('data-testid="summary-goal"');
    expect(html).toContain("Ship the viewer");
    expect(html).toContain('data-testid="summary-changes-total"');
    expect(html).toContain("+4");
  });

  test("a session with nothing to say is still not a card", () => {
    expect(render(<SessionSummaryCard {...base} />)).not.toContain("session-summary-card");
  });
});
