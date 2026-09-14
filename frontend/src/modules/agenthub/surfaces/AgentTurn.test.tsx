import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { groupTurns } from "../turns";
import type { TranscriptItem } from "../types";
import { AgentTurn } from "./AgentTurn";

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
  it("draws the question, one line for the work, then the answer", () => {
    const [turn] = groupTurns(items);
    const html = renderToString(<AgentTurn turn={turn} />);

    expect(html).toContain('data-testid="turn-prompt"');
    expect(html).toContain("Worked for 4m 7s");
    // Folded: neither the call nor the narration before it is on screen.
    expect(html).not.toContain("find . -type d");
    expect(html).not.toContain("exploring the tree");
    // The answer on the page's own ground: prose, no bubble, no name over it.
    expect(html).toContain('data-testid="agent-prose"');
    expect(html).toContain("Gradle");
    expect(html).not.toContain('data-testid="agent-message"');
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
