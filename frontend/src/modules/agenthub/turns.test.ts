import { describe, expect, it } from "bun:test";

import {
  activityOf,
  formatElapsed,
  groupTurns,
  stepLabel,
  stepSummary,
  workedFor,
  workSegments,
} from "./turns";
import type { TranscriptItem } from "./types";

let seq = 0;
const row = (kind: string, extra: Partial<TranscriptItem> = {}): TranscriptItem => ({
  id: `r${++seq}`,
  kind,
  ...extra,
});

describe("groupTurns", () => {
  it("puts the answer under its question and folds the work between them", () => {
    const [turn] = groupTurns([
      row("user_message_chunk", { text: "explain the repo" }),
      row("agent_thought_chunk", { text: "look at the tree first" }),
      row("tool_call", { toolCall: { id: "t1", title: "find ." } }),
      row("agent_message_chunk", { text: "It is a Gradle project." }),
    ]);

    expect(turn.prompt?.text).toBe("explain the repo");
    expect(turn.work.map((r) => r.kind)).toEqual(["agent_thought_chunk", "tool_call"]);
    expect(turn.answer.map((r) => r.text)).toEqual(["It is a Gradle project."]);
  });

  it("moves narration into the work once more work follows it", () => {
    // "I'll start by exploring" is not the reply. Nothing on the wire says so;
    // the tool call that comes after it does.
    const [turn] = groupTurns([
      row("user_message_chunk"),
      row("agent_message_chunk", { text: "I'll start by exploring." }),
      row("tool_call", { toolCall: { id: "t1" } }),
      row("agent_message_chunk", { text: "Done." }),
    ]);

    expect(turn.work.map((r) => r.text ?? r.kind)).toEqual(["I'll start by exploring.", "tool_call"]);
    expect(turn.answer.map((r) => r.text)).toEqual(["Done."]);
  });

  it("starts a new turn at every prompt", () => {
    const turns = groupTurns([
      row("user_message_chunk", { text: "one" }),
      row("agent_message_chunk", { text: "a" }),
      row("user_message_chunk", { text: "two" }),
      row("agent_message_chunk", { text: "b" }),
    ]);
    expect(turns.map((t) => t.prompt?.text)).toEqual(["one", "two"]);
    expect(turns.map((t) => t.answer[0]?.text)).toEqual(["a", "b"]);
  });

  it("keeps every held turn's identity when an older page lands in front", () => {
    const held = [
      row("user_message_chunk", { text: "two" }),
      row("agent_message_chunk", { text: "b" }),
      row("user_message_chunk", { text: "three" }),
      row("agent_message_chunk", { text: "c" }),
    ];
    const previous = groupTurns(held);
    const older = [row("user_message_chunk", { text: "one" }), row("agent_message_chunk", { text: "a" })];

    const next = groupTurns([...older, ...held], previous);

    expect(next).toHaveLength(3);
    expect(next[1]).toBe(previous[0]);
    expect(next[2]).toBe(previous[1]);
  });

  it("keeps what the agent said before anyone spoke", () => {
    const [turn] = groupTurns([row("agent_message_chunk", { text: "Hello" })]);
    expect(turn.prompt).toBeUndefined();
    expect(turn.answer[0]?.text).toBe("Hello");
  });

  it("sets aside rows that are neither work nor words", () => {
    const [turn] = groupTurns([row("user_message_chunk"), row("future_kind")]);
    expect(turn.notes.map((r) => r.kind)).toEqual(["future_kind"]);
    expect(turn.work).toEqual([]);
  });

  it("shares unchanged turns when the last item grows", () => {
    const initial = [
      row("user_message_chunk", { text: "one" }),
      row("agent_message_chunk", { text: "first" }),
      row("user_message_chunk", { text: "two" }),
      row("agent_message_chunk", { text: "second" }),
    ];
    const previous = groupTurns(initial);
    const last = initial[initial.length - 1];
    const next = [...initial.slice(0, -1), { ...last, text: "second chunk" }];
    const current = groupTurns(next, previous);

    expect(current[0]).toBe(previous[0]);
    expect(current[1]).not.toBe(previous[1]);
  });
});

describe("workedFor", () => {
  it("measures from the prompt to the last thing that changed", () => {
    const [turn] = groupTurns([
      row("user_message_chunk", { createdAt: "2026-09-14T10:00:00Z" }),
      row("tool_call", {
        toolCall: { id: "t1" },
        createdAt: "2026-09-14T10:00:05Z",
        updatedAt: "2026-09-14T10:04:07Z",
      }),
    ]);
    expect(workedFor(turn)).toBe("Worked for 4m 7s");
  });

  it("says only that it worked when there is no clock to read", () => {
    const [turn] = groupTurns([row("user_message_chunk"), row("tool_call", { toolCall: { id: "t" } })]);
    expect(workedFor(turn)).toBe("Worked");
  });

  it("uses the precision a person would", () => {
    expect(formatElapsed(12_400)).toBe("12s");
    expect(formatElapsed(247_000)).toBe("4m 7s");
    expect(formatElapsed(3_720_000)).toBe("1h 2m");
  });
});

describe("activityOf", () => {
  const live = (...rows: TranscriptItem[]) => activityOf(groupTurns([row("user_message_chunk"), ...rows])[0]);

  it("names the tool that is running, in its own words", () => {
    expect(live(row("tool_call", { toolCall: { id: "t", kind: "read", title: "Read go.mod" } }))).toEqual({
      label: "Read go.mod",
      orb: "searching",
    });
    expect(live(row("tool_call", { toolCall: { id: "t", kind: "edit", title: "Edit main.go" } })).orb).toBe(
      "shaping",
    );
    expect(live(row("tool_call", { toolCall: { id: "t", kind: "execute", title: "go test" } })).orb).toBe(
      "working",
    );
  });

  it("follows the turn from thinking to writing", () => {
    expect(live()).toEqual({ label: "Starting", orb: "breathing" });
    expect(live(row("agent_thought_chunk"))).toEqual({ label: "Thinking", orb: "solving" });
    expect(live(row("subagent_spawned", { subagent: { id: "s", name: "Explore" } }))).toEqual({
      label: "Running Explore",
      orb: "connecting",
    });
    expect(live(row("tool_call", { toolCall: { id: "t" } }), row("agent_message_chunk"))).toEqual({
      label: "Writing",
      orb: "composing",
    });
  });
});

describe("workSegments", () => {
  it("runs steps together until the agent speaks again", () => {
    const segments = workSegments([
      row("agent_message_chunk", { text: "Comparing the docs with the repo." }),
      row("tool_call", { toolCall: { id: "a", kind: "read" } }),
      row("agent_thought_chunk"),
      row("tool_call", { toolCall: { id: "b", kind: "fetch" } }),
      row("agent_message_chunk", { text: "" }),
      row("tool_call", { toolCall: { id: "c", kind: "execute" } }),
      row("agent_message_chunk", { text: "Only PreToolUse is wired." }),
      row("plan"),
    ]);

    expect(segments.map((s) => (s.kind === "steps" ? `steps:${s.items.length}` : s.kind))).toEqual([
      "text",
      // An empty message is not speech: it does not split the run.
      "steps:4",
      "text",
      "plan",
    ]);
  });
});

describe("stepSummary", () => {
  it("names each kind of step once, in the order it first happened", () => {
    expect(
      stepSummary([
        row("tool_call", { toolCall: { id: "a", kind: "read" } }),
        row("tool_call", { toolCall: { id: "b", kind: "read" } }),
        row("tool_call", { toolCall: { id: "c", kind: "execute" } }),
        row("tool_call", { toolCall: { id: "d", kind: "fetch" } }),
      ]),
    ).toBe("Read files, ran commands, searched the web");
    expect(stepSummary([row("agent_thought_chunk"), row("tool_call", { toolCall: { id: "e" } })])).toBe(
      "Thought, used tools",
    );
  });
});

describe("stepLabel", () => {
  // The titles claude-agent-acp 0.74 writes, verbatim in shape.
  it.each([
    [{ kind: "read", title: "Read internal/agenthub/hook.go" }, "Read", "hook.go"],
    [{ kind: "read", title: "Read docs/agent-hub.mdx (1 - 50)" }, "Read", "agent-hub.mdx"],
    [{ kind: "edit", title: "Edit frontend/src/turns.ts" }, "Edited", "turns.ts"],
    [{ kind: "edit", title: "Write notes.md" }, "Wrote", "notes.md"],
    [{ kind: "fetch", title: "Fetch https://code.claude.com/docs/en/hooks" }, "Fetched", "https://code.claude.com/docs/en/hooks"],
    [{ kind: "fetch", title: 'Search "claude code hooks"' }, "Searched the web for", "claude code hooks"],
    [{ kind: "search", title: 'grep -n "PreToolUse" internal' }, "Searched for", '-n "PreToolUse" internal'],
    [{ kind: "execute", title: "go test ./internal/agenthub/" }, "Ran", "go test ./internal/agenthub/"],
    [{ kind: "other", title: "mcp__devkit__db_listProfiles" }, "mcp__devkit__db_listProfiles", ""],
  ])("%o reads as a verb and its object", (call, verb, object) => {
    expect(stepLabel({ id: "x", ...call })).toEqual({ verb, object });
  });
});
