import { afterEach, describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { queryClient } from "@/lib/query";
import type { ToolCall, TranscriptItem } from "../types";
import { activityFeed, latestTarget } from "../follow";
import { AgentToolActivity } from "./AgentToolActivity";
import { AgentToolCall } from "./AgentToolCall";
import { toolCallDetailQuery } from "./toolCallDetail";
import "@/modules/tasks/manifest";
import "@/modules/git/manifest";

/**
 * The same DevKit tool call as each vendor sends it (workbench/toolPayload.ts):
 * Claude's result rides in content text, Codex's call has no name, and a page
 * read cuts big raw fields to a preview.
 */
const board = { id: "b1", name: "Backend", keyPrefix: "BE", color: "", workflow: { statuses: [] }, workspaceId: "ws-a" };
const claudeText = (text: string) => [{ type: "content", content: { type: "text", text } }];

const item = (call: ToolCall): TranscriptItem => ({
  id: call.id,
  kind: "tool_call",
  createdAt: new Date(Date.now() - 10_000).toISOString(),
  toolCall: call,
});

afterEach(() => queryClient.clear());

describe("tool activities, whatever the vendor sends", () => {
  it("name their target from Claude's answer, which rides in content", () => {
    const call: ToolCall = {
      id: "claude-board",
      name: "mcp__devkit__tasks_board_create",
      status: "completed",
      rawInput: { name: "Backend", keyPrefix: "be" },
      content: claudeText(JSON.stringify(board)),
    };
    expect(latestTarget(activityFeed([item(call)]))?.ref).toMatchObject({ module: "tasks", kind: "board", id: "b1" });
    expect(renderToString(<AgentToolActivity call={call} />)).toContain(">Open<");
  });

  it("find a Codex call by its devkit.<tool> title", () => {
    const call: ToolCall = {
      id: "codex-board",
      title: "devkit.tasks.board.create",
      status: "completed",
      rawInput: { name: "Backend", keyPrefix: "be" },
      rawOutput: { content: [{ type: "text", text: JSON.stringify(board) }], structuredContent: board },
    };
    expect(activityFeed([item(call)]).map((i) => i.id)).toEqual(["codex-board"]);
    expect(latestTarget(activityFeed([item(call)]))?.ref).toMatchObject({ id: "b1" });
  });

  it("read the full row when the answer on the page is a cut preview", () => {
    const call: ToolCall = {
      id: "cut-board",
      name: "mcp__devkit__tasks_board_create",
      status: "completed",
      truncated: true,
      rawInput: { name: "Backend", keyPrefix: "be" },
      rawOutput: '{"id":"b1","name":"Back',
    };
    queryClient.setQueryData(toolCallDetailQuery("s1", call.id).queryKey, { ...call, truncated: false, rawOutput: board });
    expect(renderToString(<AgentToolActivity call={call} sessionId="s1" />)).toContain(">Open<");
  });
});

describe("a failed DevKit call says why, whatever the vendor sends", () => {
  it("Claude: the fenced AppError in content", () => {
    const html = renderToString(
      <AgentToolCall
        call={{
          id: "claude-failed",
          name: "mcp__devkit__db_query",
          status: "failed",
          content: claudeText('```\nquery_failed: relation "x" does not exist\n```'),
        }}
      />,
    );
    expect(html).toContain('data-testid="tool-call-reason"');
    expect(html).toContain("query_failed: relation &quot;x&quot; does not exist</p>");
  });

  it("Codex: the {error} in rawOutput", () => {
    const html = renderToString(
      <AgentToolCall call={{ id: "codex-failed", title: "devkit.db.query", status: "failed", rawOutput: { error: "boom" } }} />,
    );
    expect(html).toContain('data-testid="tool-call-reason"');
    expect(html).toContain("boom</p>");
  });
});

describe("a done row with no view reads nothing", () => {
  it("does not parse a result nobody will draw", () => {
    let parses = 0;
    const parse = JSON.parse;
    JSON.parse = ((...args: Parameters<typeof JSON.parse>) => {
      parses += 1;
      return parse(...args);
    }) as typeof JSON.parse;
    try {
      renderToString(
        <AgentToolCall
          variant="row"
          call={{ id: "no-view", name: "mcp__devkit__vendorshapes_undrawn", status: "completed", content: claudeText('{"a":1}') }}
        />,
      );
    } finally {
      JSON.parse = parse;
    }
    expect(parses).toBe(0);
  });
});

describe("following a session reads a cut answer from the full row when it is already loaded", () => {
  it("names the target from the cached full row, and fetches nothing", () => {
    const call: ToolCall = {
      id: "cut-follow",
      name: "mcp__devkit__tasks_board_create",
      status: "completed",
      truncated: true,
      rawInput: { name: "Backend", keyPrefix: "be" },
      rawOutput: '{"id":"b1","name":"Back',
    };
    const row = { ...item(call), sessionId: "s1" };
    expect(latestTarget(activityFeed([row]))).toBeNull();
    queryClient.setQueryData(toolCallDetailQuery("s1", call.id).queryKey, { ...call, truncated: false, rawOutput: board });
    expect(latestTarget(activityFeed([row]))?.ref).toMatchObject({ module: "tasks", kind: "board", id: "b1" });
  });
});
