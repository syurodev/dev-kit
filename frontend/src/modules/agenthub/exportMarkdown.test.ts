import { describe, expect, it } from "bun:test";

import { stepLine, transcriptToMarkdown } from "./exportMarkdown";
import type { TranscriptItem } from "./types";

const items: TranscriptItem[] = [
  { id: "p1", kind: "user_message_chunk", text: "Why does the build fail?\nIt worked yesterday." },
  { id: "t1", kind: "agent_thought_chunk", text: "Let me look." },
  {
    id: "c1",
    kind: "tool_call",
    toolCallId: "c1",
    toolCall: { id: "c1", title: "Read package.json", kind: "read", status: "completed" },
  },
  {
    id: "c2",
    kind: "tool_call",
    toolCallId: "c2",
    toolCall: { id: "c2", title: "bun test", kind: "execute", status: "failed" },
  },
  { id: "s1", kind: "subagent_spawned", subagent: { id: "s1", name: "Explore", task: "Map the repo", state: "completed" } },
  { id: "a1", kind: "async_task_spawned", asyncTask: { id: "a1", name: "Lint", state: "running" } },
  { id: "m1", kind: "agent_message_chunk", text: "The lockfile is stale.\n\nRun `bun install`." },
  { id: "p2", kind: "user_message_chunk", text: "done" },
  { id: "m2", kind: "agent_message_chunk", text: "Great." },
];

describe("transcriptToMarkdown", () => {
  it("writes one section per turn: the prompt quoted, the work listed, the answer as prose", () => {
    const md = transcriptToMarkdown(items, {
      title: "Build failure",
      projectPath: "/repo/app",
      vendor: "claude",
    });

    expect(md).toStartWith("# Build failure\n\n`claude` · `/repo/app`\n");
    expect(md).toContain("## Turn 1\n\n**You:**\n\n> Why does the build fail?\n> It worked yesterday.");
    expect(md).toContain("**Work:**\n\n- Thought\n- `Read package.json`\n- `bun test` (failed)\n- Explore — Map the repo (completed)\n- Lint (running)");
    expect(md).toContain("**Agent:**\n\nThe lockfile is stale.\n\nRun `bun install`.");
    expect(md).toContain("## Turn 2\n\n**You:**\n\n> done\n\n**Agent:**\n\nGreat.");
    expect(md.endsWith("\n")).toBe(true);
    expect(md.split("## Turn ")).toHaveLength(3);
  });

  it("handles a title with nothing in it and a transcript with nothing in it", () => {
    expect(transcriptToMarkdown([], { title: "  " })).toBe("# Agent session\n");
  });

  it("fences a tool title that carries backticks itself", () => {
    expect(stepLine({ id: "x", kind: "tool_call", toolCall: { id: "x", title: "echo `hi`" } })).toBe("`` echo `hi` ``");
  });
});
