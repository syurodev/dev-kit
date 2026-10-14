import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { groupTurns } from "../turns";
import type { TranscriptItem } from "../types";
import { AgentTurn, StepGroup } from "./AgentTurn";
import { AgentFileChip } from "../transcript/AgentFileChip";

const items: TranscriptItem[] = [
  { id: "u", kind: "user_message_chunk", text: "explain the repo", createdAt: "2026-09-14T10:00:00Z" },
  { id: "n", kind: "agent_message_chunk", text: "Starting by exploring the tree." },
  {
    id: "t",
    kind: "tool_call",
    toolCall: { id: "c", kind: "execute", title: "find . -type d", status: "completed" },
    createdAt: "2026-09-14T10:00:01Z",
    updatedAt: "2026-09-14T10:04:07Z",
  },
  { id: "a", kind: "agent_message_chunk", text: "## Layout\n\nIt is a **Gradle** project." },
];

describe("AgentTurn", () => {
  it("draws the question, the account of the work, then the answer", () => {
    const [turn] = groupTurns(items);
    const html = renderToString(<AgentTurn turn={turn} />);

    expect(html).toContain('data-testid="turn-prompt"');
    expect(html).toContain("Worked for 4m 7s");
    // What the agent said between steps was said to you: it is never folded,
    // even though it happened before the last tool call.
    expect(html).toContain("exploring the tree");
    // A lone step needs no fold either — its own row is already one line.
    expect(html).toContain("find . -type d");
    expect(html).not.toContain('data-testid="step-group"');
    // The answer on the page's own ground: prose, no bubble, no name over it.
    expect(html).toContain('data-testid="agent-prose"');
    expect(html).toContain("Gradle");
    expect(html).not.toContain('data-testid="agent-message"');
  });

  it("carries a pasted token past the bubble's own max-width, not just break-words", () => {
    // overflow-wrap: break-word (Tailwind's break-words) is excluded from
    // the browser's min-content calculation by spec — a flex ancestor
    // without its own min-w-0 still sizes off the unbroken run and forces
    // the bubble open sideways even with break-words set on it. wrap-anywhere
    // (overflow-wrap: anywhere) counts toward min-content, so it actually
    // holds regardless of the ancestor chain.
    const token = "eyJ" + "a".repeat(400) + "." + "b".repeat(200);
    const [turn] = groupTurns([
      { id: "u", kind: "user_message_chunk", text: `token mẫu là ${token}`, createdAt: "2026-09-14T10:00:00Z" },
    ]);
    const html = renderToString(<AgentTurn turn={turn} />);
    expect(html).toContain("wrap-anywhere");
    expect(html).not.toContain("break-words");
  });

  it("keeps a run of several steps folded, even with words on both sides of it", () => {
    const read = (id: string, title: string): TranscriptItem => ({
      id,
      kind: "tool_call",
      toolCall: { id, kind: "read", title, status: "completed" },
    });
    const [turn] = groupTurns([
      { id: "u", kind: "user_message_chunk", text: "explain the repo" },
      { id: "n1", kind: "agent_message_chunk", text: "Looking at a few files first." },
      read("a", "Read go.mod"),
      read("b", "Read main.go"),
      { id: "n2", kind: "agent_message_chunk", text: "It is a Go module." },
    ]);
    const html = renderToString(<AgentTurn turn={turn} />);

    // Both sentences are on the page — neither is narration hidden by a fold.
    expect(html).toContain("Looking at a few files first.");
    expect(html).toContain("It is a Go module.");
    // The run between them is still behind its own fold, closed until asked for.
    expect(html).toContain('data-testid="step-group"');
    expect(html).toContain('data-state="collapsed"');
    expect(html).not.toContain("go.mod");
    expect(html).not.toContain("main.go");
  });

  it("follows a live turn's current step beside the orb", () => {
    const [live] = groupTurns(items.slice(0, 3));
    const html = renderToString(<AgentTurn turn={live} live latest />);

    // The tool's own title — reassurance is specific, "Working…" is not.
    expect(html).toContain("find . -type d");
    expect(html).toContain("<canvas");
    expect(html).not.toContain("Worked for");
  });

  it("shows the orb only while the turn runs", () => {
    const [turn] = groupTurns(items);
    expect(renderToString(<AgentTurn turn={turn} latest />)).not.toContain("<canvas");
  });
});

describe("StepGroup", () => {
  const read = (id: string, title: string): TranscriptItem => ({
    id,
    kind: "tool_call",
    toolCall: { id, kind: "read", title, status: "completed" },
  });

  it("is closed by default — nothing above it hides the steps anymore", () => {
    const html = renderToString(
      <StepGroup
        items={[
          read("a", "Read internal/agenthub/hook.go"),
          { id: "b", kind: "tool_call", toolCall: { id: "b", kind: "execute", title: "go test ./...", status: "completed" } },
        ]}
      />,
    );
    expect(html).toContain('data-testid="step-group"');
    expect(html).toContain('data-state="collapsed"');
    expect(html).toContain("Read files, ran commands");
    expect(html).not.toContain("hook.go");
  });

  it("gathers a run of steps under what kind of work it was, as plain rows", () => {
    const html = renderToString(
      <StepGroup
        defaultOpen
        items={[
          read("a", "Read internal/agenthub/hook.go"),
          { id: "b", kind: "tool_call", toolCall: { id: "b", kind: "execute", title: "go test ./...", status: "completed" } },
        ]}
      />,
    );
    expect(html).toContain('data-testid="step-group"');
    expect(html).toContain('data-state="expanded"');
    expect(html).toContain("Read files, ran commands");
    // Rows, not cards: a verb and what it acted on.
    expect(html).toContain('data-variant="row"');
    expect(html).toContain("hook.go");
    expect(html).not.toContain("internal/agenthub/hook.go");
  });

  it("draws a subagent that did something as a closed toggle over its steps", () => {
    const [turn] = groupTurns([
      { id: "u", kind: "user_message_chunk", text: "go" },
      { id: "s", kind: "subagent_spawned", subagent: { id: "a1", name: "Write the handler", task: "Repo: /work\n\nBrief…" } },
      { id: "n", kind: "agent_message_chunk", text: "I'll start by reading.", messageId: "m-sub", origin: "a1" },
      { id: "t1", kind: "tool_call", origin: "a1", toolCall: { id: "t1", kind: "read", title: "Read secret-step.lua", status: "completed" } },
      { id: "t2", kind: "tool_call", origin: "a1", toolCall: { id: "t2", kind: "read", title: "Read b.lua", status: "completed" } },
      { id: "a", kind: "agent_message_chunk", text: "Done.", messageId: "m-lead" },
    ]);
    const html = renderToString(<AgentTurn turn={turn} />);

    const row = html.match(/<button[^>]*data-testid="agent-subagent"[^>]*>[\s\S]*?<\/button>/)?.[0] ?? "";
    expect(row).toContain('aria-expanded="false"');
    expect(row).toContain("Write the handler");
    expect(row).toContain("2 steps");
    // The brief's first line is not what a row should say.
    expect(row).not.toContain("Repo: /work");
    // Its steps and narration are behind the row, not in the lead agent's account.
    expect(html).not.toContain("secret-step.lua");
    expect(html).not.toContain("start by reading");
  });

  it("draws a single step as its row alone", () => {
    const html = renderToString(<StepGroup items={[{ id: "t", kind: "agent_thought_chunk", text: "hm" }]} />);
    // "Thought" over a row reading "Thought" was the same word twice.
    expect(html).not.toContain('data-testid="step-group"');
    expect(html).toContain('data-testid="agent-thinking"');
  });
});

describe("message actions", () => {
  it("gives the prompt and the answer an inline row that shows only when pointed at", () => {
    const [turn] = groupTurns(items);
    const html = renderToString(<AgentTurn turn={turn} onCopy={() => {}} onEditPrompt={() => {}} />);

    // No floating card anywhere; each message carries its own row.
    expect(html).not.toContain("hover-card");
    const rows = html.match(/<div[^>]*data-testid="message-actions"[^>]*>/g) ?? [];
    expect(rows.length).toBe(2);
    // Invisible until hovered or focused, but holding its place.
    for (const row of rows) expect(row).toContain("opacity-0");
  });

  it("reaches the actions from the keyboard through the buttons themselves", () => {
    const [turn] = groupTurns(items);
    const html = renderToString(<AgentTurn turn={turn} onCopy={() => {}} onEditPrompt={() => {}} />);

    // The message is no longer a tab stop that does nothing; Copy and Edit are.
    expect(html).not.toContain('aria-label="Message: explain the repo"');
    expect(html).toContain('aria-label="Copy message"');
    expect(html).toContain('aria-label="Edit in composer"');
  });
});

describe("a prompt of only attachments", () => {
  it("is still its own turn, and its bubble names what it carried", () => {
    const turns = groupTurns([
      { id: "u1", kind: "user_message_chunk", text: "first", messageId: "p1" },
      { id: "a1", kind: "agent_message_chunk", text: "ok", messageId: "msg_1" },
      { id: "u2", kind: "user_message_chunk", text: "", messageId: "p2", attachments: [{ name: "screen.png", kind: "image" }, { name: "notes.md", kind: "file" }] },
      { id: "a2", kind: "agent_message_chunk", text: "I see a screenshot", messageId: "msg_2" },
    ]);
    expect(turns).toHaveLength(2);
    expect(turns[1].prompt?.messageId).toBe("p2");

    const html = renderToString(<AgentTurn turn={turns[1]} onCopy={() => {}} />);
    expect(html).toContain('data-testid="turn-prompt-attachments"');
    expect(html).toContain("screen.png");
    expect(html).toContain("notes.md");
    // No empty text bubble for a prompt that had no words.
    expect(html).not.toContain('data-testid="turn-prompt"');
  });
});

describe("a PDF a prompt carried", () => {
  it("is named with its page count, so the person can see it was kept as a document", () => {
    const turns = groupTurns([
      {
        id: "u1",
        kind: "user_message_chunk",
        text: "Điều nào quy định xác thực?",
        messageId: "p1",
        attachments: [
          { name: "Thông tư 64.pdf", kind: "document", documentId: "doc1", pages: 45 },
          { name: "một trang.pdf", kind: "document", documentId: "doc2", pages: 1 },
        ],
      },
    ]);
    const html = renderToString(<AgentTurn turn={turns[0]} onCopy={() => {}} />);
    expect(html).toContain("Thông tư 64.pdf");
    expect(html).toContain("45 pages");
    expect(html).toContain("1 page<");
    expect(html).not.toContain("1 pages");
  });
});

describe("transcript details", () => {
  it("attachments are cards above the prompt", () => {
    const turns = groupTurns([
      {
        id: "u1",
        kind: "user_message_chunk",
        text: "read this",
        messageId: "p1",
        attachments: [
          { name: "spec-auth-v3.pdf", kind: "document", documentId: "doc1", pages: 12 },
          { name: "screen.png", kind: "image" },
          { name: "notes.md", kind: "file" },
        ],
      },
    ]);
    const html = renderToString(<AgentTurn turn={turns[0]} onCopy={() => {}} />);
    expect(html).toContain('data-testid="turn-attachment-card"');
    expect(html).toContain(">spec-auth-v3.pdf<");
    expect(html).toContain(">PDF · 12 pages<");
    expect(html).toContain(">Image<");
    expect(html).toContain(">MD<");
    // The cards come before the bubble.
    expect(html.indexOf("turn-attachment-card")).toBeLessThan(html.indexOf('data-testid="turn-prompt"'));
  });

  it("a single page is not '1 pages'", () => {
    const turns = groupTurns([
      { id: "u1", kind: "user_message_chunk", text: "", messageId: "p1", attachments: [{ name: "one.pdf", kind: "document", pages: 1 }] },
    ]);
    const html = renderToString(<AgentTurn turn={turns[0]} onCopy={() => {}} />);
    expect(html).toContain(">PDF · 1 page<");
  });

  it("the work line keeps 'Worked for' with a hairline and a chevron", () => {
    const [turn] = groupTurns(items);
    const html = renderToString(<AgentTurn turn={turn} />);
    expect(html).toMatch(/data-testid="turn-work"[\s\S]*border-b border-border-subtle[\s\S]*Worked for 4m 7s[\s\S]*lucide-chevron-right/);
  });
});

describe("AgentFileChip", () => {
  it("shows its detail when it is a button too", () => {
    const html = renderToString(<AgentFileChip path="a.pdf" detail="6 pages" onClick={() => {}} />);
    expect(html).toContain("6 pages");
  });
});

describe("the plan card when the summary is docked", () => {
  const planned = (status: string): TranscriptItem[] => [
    { id: "u", kind: "user_message_chunk", text: "go", createdAt: "2026-09-14T10:00:00Z" },
    {
      id: "p",
      kind: "plan",
      plan: [
        { content: "a", status: "completed" },
        { content: "b", status: "completed" },
        { content: "c", status: status },
        { content: "d", status: "pending" },
      ],
    },
    { id: "a", kind: "agent_message_chunk", text: "Done." },
  ];

  it("the latest plan is one quiet line when docked, and the full card is hidden then", () => {
    const [turn] = groupTurns(planned("in_progress"));
    const html = renderToString(<AgentTurn turn={turn} latestPlan />);
    expect(html).toMatch(/data-testid="turn-plan-docked"[^>]*class="hidden [^"]*@6xl\/agent-conversation:flex/);
    expect(html).toContain("Plan · 2 of 4 — in the summary");
    expect(html).toMatch(/data-testid="agent-plan"[^>]*@6xl\/agent-conversation:hidden/);
  });

  it("an older turn's plan has no docked line and keeps its card", () => {
    const [turn] = groupTurns(planned("in_progress"));
    const html = renderToString(<AgentTurn turn={turn} />);
    expect(html).not.toContain("turn-plan-docked");
    expect(html).not.toMatch(/data-testid="agent-plan"[^>]*@6xl\/agent-conversation:hidden/);
  });

  it("a latest plan with no entries keeps the normal card and draws no docked line", () => {
    const [turn] = groupTurns([
      { id: "u", kind: "user_message_chunk", text: "go", createdAt: "2026-09-14T10:00:00Z" },
      { id: "p", kind: "plan", plan: [] },
      { id: "a", kind: "agent_message_chunk", text: "Done." },
    ]);
    const html = renderToString(<AgentTurn turn={turn} latestPlan />);
    expect(html).not.toContain("turn-plan-docked");
    expect(html).toContain('data-testid="agent-plan"');
    expect(html).not.toMatch(/data-testid="agent-plan"[^>]*@6xl\/agent-conversation:hidden/);
  });
});
