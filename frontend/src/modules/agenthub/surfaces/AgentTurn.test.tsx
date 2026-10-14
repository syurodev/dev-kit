import { afterAll, beforeAll, describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { groupTurns } from "../turns";
import type { TranscriptItem } from "../types";
import { AgentTurn, StepGroup, stepRunLayout, SubagentActivity } from "./AgentTurn";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { registerToolActivity } from "@/workbench/toolActivity";
import { AgentFileChip } from "../transcript/AgentFileChip";
import { TranscriptActionsProvider } from "../transcript/transcriptActions";

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
  it("links symbols and commits in what the agent said only once the turn is over", () => {
    const [turn] = groupTurns([
      { id: "u", kind: "user_message_chunk", text: "where?", createdAt: "2026-09-14T10:00:00Z" },
      { id: "n", kind: "agent_message_chunk", text: "Looking at `Service.Invoke`." },
      {
        id: "t",
        kind: "tool_call",
        toolCall: { id: "c", kind: "read", title: "read", status: "completed" },
        createdAt: "2026-09-14T10:00:01Z",
      },
      { id: "a", kind: "agent_message_chunk", text: "It is `Service.Invoke`, since `a1b2c3d`." },
    ]);
    const hit = { workspaceId: "ws", folderId: "api", folderName: "api", filePath: "service.go", line: 3, kind: "method", label: "Service.Invoke" };
    const render = (live: boolean) =>
      renderToString(
        <TranscriptActionsProvider
          value={{ refContext: { sessionId: "s", workspaceId: "ws", projectPath: "/repo" }, lookupSymbol: () => hit, openRef: () => {} }}
        >
          <AgentTurn turn={turn} live={live} />
        </TranscriptActionsProvider>,
      );
    const done = render(false);
    // The words between steps and the answer both.
    expect(done.match(/data-testid="agent-symbol-ref"/g)?.length).toBe(2);
    expect(done).toContain('data-testid="agent-commit-ref"');
    const live = render(true);
    expect(live).not.toContain("agent-symbol-ref");
    expect(live).not.toContain("agent-commit-ref");
  });

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

describe("results outside the fold", () => {
  const disposers: (() => void)[] = [];
  beforeAll(() => {
    // A view registered the way an owning module would, for a tool no module owns.
    disposers.push(
      registerToolResultView("x.view", (input) => {
        // Shape checked first, the way a real view does: anything else declines.
        const n = (input.rawOutput as { n?: unknown } | null)?.n;
        return typeof n === "number" ? <div data-testid="fake-view">{`value ${n}`}</div> : null;
      }),
    );
    // A view that declines: the card it would have been draws nothing.
    disposers.push(registerToolResultView("x.none", () => null));
    // A tool that changes something and also has a view: its activity card is its result.
    disposers.push(registerToolResultView("x.act", () => <div data-testid="fake-act-view">acted</div>));
    disposers.push(
      registerToolActivity("x.act", {
        module: "x",
        running: "Acting",
        done: "Acted",
        failed: "Couldn't act",
        target: () => null,
      }),
    );
  });
  afterAll(() => {
    for (const dispose of disposers.splice(0)) dispose();
  });

  const viewed = (id: string, n: number, status = "completed"): TranscriptItem => ({
    id,
    kind: "tool_call",
    toolCall: { id, name: "x.view", kind: "other", title: `View ${id}`, status, rawOutput: { n } },
  });
  const declined = (id: string): TranscriptItem => ({
    id,
    kind: "tool_call",
    toolCall: { id, name: "x.none", kind: "other", title: "Nothing to show", status: "completed", rawOutput: { n: 1 } },
  });
  const other: TranscriptItem = {
    id: "o",
    kind: "tool_call",
    toolCall: { id: "o", kind: "read", title: "Read other.go", status: "completed" },
  };
  const count = (html: string, needle: string) => html.split(needle).length - 1;
  /** What sits after the fold: everything past the collapse's closing toggle. */
  const afterFold = (html: string) => html.slice(html.indexOf("</button>"));

  it("shows done results with a view after the fold, three at most, in call order", () => {
    const html = renderToString(
      <StepGroup items={[viewed("a", 1), other, viewed("b", 2), viewed("c", 3), viewed("d", 4)]} />,
    );
    expect(html).toContain('data-testid="step-group"');
    expect(count(html, 'data-testid="turn-result-card"')).toBe(3);
    const after = afterFold(html);
    expect(after.indexOf("value 1")).toBeLessThan(after.indexOf("value 2"));
    expect(after.indexOf("value 2")).toBeLessThan(after.indexOf("value 3"));
    expect(after).not.toContain("value 4");
    // The rest are counted, and the count opens the fold.
    const more = html.match(/<button[^>]*data-testid="step-results-more"[^>]*>[^<]*/)?.[0] ?? "";
    expect(more).toContain("+1 more result in the work above");
  });

  it("says results, plural, when more than one stays behind", () => {
    const html = renderToString(
      <StepGroup items={[viewed("a", 1), viewed("b", 2), viewed("c", 3), viewed("d", 4), viewed("e", 5)]} />,
    );
    expect(html).toContain("+2 more results in the work above");
  });

  it("still lists every step in the fold", () => {
    const html = renderToString(
      <StepGroup defaultOpen items={[viewed("a", 1), other, viewed("b", 2), viewed("c", 3), viewed("d", 4)]} />,
    );
    expect(count(html, 'data-variant="row"')).toBe(5);
    expect(html).toContain("other.go");
  });

  it("puts the cards before the run's activity cards", () => {
    const build: TranscriptItem = {
      id: "p",
      kind: "tool_call",
      toolCall: { id: "p", kind: "execute", title: "go build ./...", status: "in_progress" },
    };
    const act: TranscriptItem = {
      id: "q",
      kind: "tool_call",
      toolCall: { id: "q", name: "x.act", kind: "other", title: "act", status: "in_progress" },
    };
    const html = renderToString(<StepGroup items={[viewed("a", 1), other, act, build]} />);
    const activities = html.indexOf('data-testid="step-activities"');
    expect(activities).toBeGreaterThan(-1);
    expect(html.indexOf('data-testid="tool-activity"')).toBeGreaterThan(activities);
    expect(html.indexOf('data-testid="turn-result-card"')).toBeGreaterThan(-1);
    expect(html.indexOf('data-testid="turn-result-card"')).toBeLessThan(activities);
  });

  it("leaves calls that have not completed in the fold", () => {
    for (const status of ["in_progress", "failed", undefined, "cancelled", "pending"]) {
      // Built by hand: a default parameter would turn `undefined` back into "completed".
      const item = viewed("a", 1);
      const call = { ...item.toolCall!, status };
      const html = renderToString(<StepGroup items={[{ ...item, toolCall: call }, other]} />);
      expect(html).not.toContain('data-testid="turn-result-card"');
      expect(html).not.toContain('data-testid="step-results"');
    }
  });

  it("does not surface an activity's result twice", () => {
    const act: TranscriptItem = {
      id: "q",
      kind: "tool_call",
      toolCall: { id: "q", name: "x.act", kind: "other", title: "act", status: "completed", rawOutput: { ok: true } },
    };
    const html = renderToString(<StepGroup items={[act, other]} />);
    expect(html).toContain('data-testid="tool-activity"');
    expect(html).not.toContain('data-testid="turn-result-card"');
  });

  it("surfaces a Codex call, which has no name, by its title", () => {
    const codex: TranscriptItem = {
      id: "k",
      kind: "tool_call",
      toolCall: { id: "k", kind: "other", title: "devkit.x.view", status: "completed", rawOutput: { structuredContent: { n: 9 }, content: [] } },
    };
    const html = renderToString(<StepGroup items={[codex, other]} />);
    expect(html).toContain('data-testid="turn-result-card"');
    expect(html).toContain("value 9");
  });

  it("draws a lone result with a view as its card, not a closed row", () => {
    const html = renderToString(<StepGroup items={[viewed("a", 7)]} />);
    expect(html).toContain('data-testid="turn-result-card"');
    expect(html).toContain("value 7");
    expect(html).not.toContain('data-variant="row"');
    expect(html).not.toContain('data-testid="step-group"');
  });

  it("keeps a lone card where it was when a second step joins the run", () => {
    // Fixed slots in a fixed order, so the results slot is the same element
    // whether the run has one step or two, and React keeps the card mounted.
    const a = viewed("a", 7);
    expect(stepRunLayout({ items: [a], shown: [a], hidden: [], activities: 0, commands: 0 })).toEqual({
      lone: true,
      head: "none",
      fold: false,
      results: true,
      activities: false,
    });
    expect(stepRunLayout({ items: [a, other], shown: [a], hidden: [], activities: 0, commands: 0 })).toEqual({
      lone: false,
      head: "summary",
      fold: true,
      results: true,
      activities: false,
    });
    // A lone step without a card is its row.
    expect(stepRunLayout({ items: [other], shown: [], hidden: [], activities: 0, commands: 0 }).head).toBe("row");
    // Results left in the fold still get their line.
    expect(stepRunLayout({ items: [a, other], shown: [], hidden: [a], activities: 0, commands: 0 }).results).toBe(true);
  });

  it("marks each row of the fold with its step, for the line that points into it", () => {
    const html = renderToString(<StepGroup defaultOpen items={[viewed("a", 1), other]} />);
    expect(html).toContain('data-step-id="a"');
    expect(html).toContain('data-step-id="o"');
    expect(html).not.toContain('class="contents"');
  });

  it("hides a run whose only step draws nothing", () => {
    const html = renderToString(<StepGroup items={[{ id: "s", kind: "subagent_spawned" }]} />);
    expect(html).toMatch(/^<div[^>]*data-step-run[^>]*class="[^"]*empty:hidden/);
  });

  it("draws no result wrapper for a view that declines, and keeps a lone call's row", () => {
    expect(renderToString(<StepGroup items={[declined("z")]} />)).toContain('data-variant="row"');
    const html = renderToString(<StepGroup items={[declined("z"), other]} />);
    expect(html).not.toContain('data-testid="step-results"');
    expect(html).not.toContain('data-testid="turn-result-card"');
  });

  it("gives a declined view no slot", () => {
    const html = renderToString(
      <StepGroup items={[declined("z"), viewed("a", 1), viewed("b", 2), viewed("c", 3)]} />,
    );
    expect(count(html, 'data-testid="turn-result-card"')).toBe(3);
    expect(html).not.toContain("more result");
  });

  it("caps results per turn, not per run of steps", () => {
    const [turn] = groupTurns([
      { id: "u", kind: "user_message_chunk", text: "go" },
      viewed("a", 1),
      { id: "n1", kind: "agent_message_chunk", text: "Now the rest.", messageId: "m1" },
      viewed("b", 2),
      viewed("c", 3),
      viewed("d", 4),
      { id: "n2", kind: "agent_message_chunk", text: "Done.", messageId: "m2" },
    ]);
    const html = renderToString(<AgentTurn turn={turn} />);
    expect(count(html, 'data-testid="turn-result-card"')).toBe(3);
    expect(html).toContain("value 1");
    expect(html).toContain("value 3");
    expect(html).not.toContain("value 4");
    expect(html).toContain("+1 more result in the work above");
  });

  it("mentions a run's results even when the turn's cards are spent", () => {
    const [turn] = groupTurns([
      { id: "u", kind: "user_message_chunk", text: "go" },
      viewed("a", 1),
      viewed("b", 2),
      viewed("c", 3),
      { id: "n1", kind: "agent_message_chunk", text: "Now the rest.", messageId: "m1" },
      viewed("d", 4),
      other,
      viewed("e", 5),
      { id: "n2", kind: "agent_message_chunk", text: "Done.", messageId: "m2" },
    ]);
    const html = renderToString(<AgentTurn turn={turn} />);
    expect(count(html, 'data-testid="turn-result-card"')).toBe(3);
    expect(html).toContain("+2 more results in the work above");
  });

  it("draws a card while the full row of a cut preview loads", () => {
    const cut: TranscriptItem = {
      id: "t",
      kind: "tool_call",
      sessionId: "s1",
      toolCall: {
        id: "t",
        name: "x.view",
        kind: "other",
        title: "View big",
        status: "completed",
        truncated: true,
        // A page read's preview: the first bytes of the JSON, which no longer parse.
        rawOutput: '{"n": 1, "rows": [[1,2],[3',
      },
    };
    const html = renderToString(<StepGroup items={[cut]} />);
    expect(html).toContain('data-testid="turn-result-card"');
    expect(html).toContain('data-testid="turn-result-loading"');
    expect(html).toContain("View big");
    // No session to load it from: nothing to wait for, so no card.
    const orphan = renderToString(<StepGroup items={[{ ...cut, sessionId: undefined }, other]} />);
    expect(orphan).not.toContain('data-testid="turn-result-card"');
  });

  it("surfaces nothing from a subagent's work", () => {
    const html = renderToString(
      <SubagentActivity
        subagent={{ id: "s1", name: "Look" }}
        activity={[viewed("a", 1), other, viewed("b", 2)]}
        brief={false}
      />,
    );
    expect(html).not.toContain('data-testid="turn-result-card"');
    const lone = renderToString(
      <SubagentActivity subagent={{ id: "s1", name: "Look" }} activity={[viewed("a", 1)]} brief={false} />,
    );
    expect(lone).not.toContain('data-testid="turn-result-card"');
    expect(lone).toContain('data-variant="row"');
  });
});
