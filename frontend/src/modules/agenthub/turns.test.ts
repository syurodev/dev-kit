import { describe, expect, it } from "bun:test";

import {
  activityOf,
  currentStep,
  formatElapsed,
  groupTurns,
  runningSubagents,
  stepCount,
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

describe("groupTurns with subagents running in the same pipe", () => {
  // Shapes from a captured claude-agent-acp session: a subagent's rows carry
  // its id as origin, and land between the chunks of the lead agent's message.
  const spawn = (id: string) =>
    row("subagent_spawned", { subagent: { id, name: `agent ${id}`, task: "do it" } });
  const said = (text: string, messageId: string, origin?: string) =>
    row("agent_message_chunk", { text, messageId, ...(origin ? { origin } : {}) });
  const step = (id: string, origin?: string) =>
    row("tool_call", { toolCall: { id, title: `Read ${id}.lua`, kind: "read" }, ...(origin ? { origin } : {}) });

  it("keeps a subagent's rows out of the lead agent's account", () => {
    const [turn] = groupTurns([
      row("user_message_chunk", { text: "go" }),
      spawn("a1"),
      said("I'll start by reading.", "m-sub", "a1"),
      step("t1", "a1"),
      said("Report: done.", "m-sub-2", "a1"),
      said("Both agents are done.", "m-lead"),
    ]);

    expect(turn.work.map((r) => r.subagent?.id ?? r.text)).toEqual(["a1"]);
    expect(turn.answer.map((r) => r.text)).toEqual(["Both agents are done."]);
    expect(turn.subagents.a1.map((r) => r.text ?? r.toolCall?.id)).toEqual(["I'll start by reading.", "t1", "Report: done."]);
  });

  it("puts a message back together when a subagent's row landed in the middle of it", () => {
    const [turn] = groupTurns([
      row("user_message_chunk", { text: "go" }),
      spawn("a1"),
      said("Trong lúc chờ, tôi sửa một ghi nhớ đ", "m1"),
      step("t1", "a1"),
      said("ã sai.", "m1"),
    ]);

    expect(turn.answer.map((r) => r.text)).toEqual(["Trong lúc chờ, tôi sửa một ghi nhớ đã sai."]);
  });

  it("does not join two messages that only happen to be next to each other", () => {
    const [turn] = groupTurns([row("user_message_chunk"), said("One.", "m1"), said("Two.", "m2")]);
    expect(turn.answer.map((r) => r.text)).toEqual(["One.", "Two."]);
  });

  it("keeps a row whose subagent is not in view in the flow", () => {
    // Its spawn is on an older page that has not loaded: dropping the row would
    // make it vanish until then.
    const [turn] = groupTurns([row("user_message_chunk"), step("t1", "gone"), said("ok", "m1")]);
    expect(turn.work.map((r) => r.toolCall?.id)).toEqual(["t1"]);
  });

  it("drops the Agent tool's empty update and keeps other bare rows", () => {
    const [turn] = groupTurns([
      row("user_message_chunk"),
      row("tool_call_update", { toolCall: { id: "spawner", name: "Agent" } }),
      spawn("a1"),
      row("tool_call", { toolCall: { id: "bare" } }),
      said("ok", "m1"),
    ]);
    expect(turn.work.map((r) => r.toolCall?.id ?? r.subagent?.id)).toEqual(["a1", "bare"]);
  });

  it("gathers a nested subagent's rows with the turn that spawned its parent", () => {
    const [turn] = groupTurns([
      row("user_message_chunk"),
      spawn("a1"),
      row("subagent_spawned", { subagent: { id: "a2", name: "inner" }, origin: "a1" }),
      step("t2", "a2"),
      said("done", "m1"),
    ]);
    expect(Object.keys(turn.subagents).sort()).toEqual(["a1", "a2"]);
    expect(turn.subagents.a2.map((r) => r.toolCall?.id)).toEqual(["t2"]);
  });

  it("keeps a turn's identity when its subagents did nothing new", () => {
    const items = [row("user_message_chunk"), spawn("a1"), step("t1", "a1"), said("ok", "m1")];
    const first = groupTurns(items);
    const again = groupTurns([...items], first);
    expect(again[0]).toBe(first[0]);

    const more = groupTurns([...items, step("t2", "a1")], first);
    expect(more[0]).not.toBe(first[0]);
    expect(more[0].subagents.a1.map((r) => r.toolCall?.id)).toEqual(["t1", "t2"]);
  });

  it("keeps an earlier turn whose message came in three pieces while a later one streams", () => {
    // Joining piece by piece cached [a, b] and then [a, b, c]; the next
    // grouping missed and every such turn re-rendered on every token.
    const items = [
      row("user_message_chunk"),
      spawn("a1"),
      said("a", "m1"),
      step("t1", "a1"),
      said("b", "m1"),
      step("t2", "a1"),
      said("c", "m1"),
      row("user_message_chunk"),
      said("live", "m2"),
    ];
    const first = groupTurns(items);
    const last = items[items.length - 1];
    const next = groupTurns([...items.slice(0, -1), { ...last, text: "live more" }], first);
    expect(next[0]).toBe(first[0]);
    expect(next[0].answer[0].text).toBe("abc");
  });

  it("keeps a re-joined message's identity while its pieces are unchanged", () => {
    const items = [row("user_message_chunk"), spawn("a1"), said("a", "m1"), step("t1", "a1"), said("b", "m1")];
    const first = groupTurns(items);
    const again = groupTurns([...items], first);
    expect(again[0].answer[0]).toBe(first[0].answer[0]);
    expect(again[0]).toBe(first[0]);
  });
});

describe("runningSubagents", () => {
  it("lists the subagents without an end, nested ones after their parent, with what each did", () => {
    const { running, activity } = runningSubagents([
      row("subagent_spawned", { subagent: { id: "a1", name: "one" } }),
      // Spawn and state update, paired into one row the way the transcript model pairs them.
      row("subagent_spawned", { subagent: { id: "a2", name: "two", state: "completed" } }),
      row("subagent_spawned", { subagent: { id: "a3", name: "inner" }, origin: "a1" }),
      row("tool_call", { origin: "a1", toolCall: { id: "t1", kind: "read", title: "Read x.lua" } }),
    ]);
    expect(running.map((entry) => entry.subagent.id)).toEqual(["a1", "a3"]);
    expect(running[0].activity.map((r) => r.subagent?.id ?? r.toolCall?.id)).toEqual(["a3", "t1"]);
    expect(Object.keys(activity).sort()).toEqual(["a1"]);
  });

  it("says what a subagent is doing now in its step's own words", () => {
    expect(currentStep([])).toBe("Starting");
    expect(currentStep([row("tool_call", { toolCall: { id: "t", kind: "read", title: "Read src/a.lua" } })])).toBe("Read a.lua");
    expect(currentStep([row("agent_message_chunk", { text: "Done." })])).toBe("Writing");
    expect(stepCount([row("agent_message_chunk"), row("tool_call", { toolCall: { id: "t" } })])).toBe(1);
  });
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

  it("does not call a compaction a subagent, though claude-agent-acp 0.78 gives both the think kind", () => {
    expect(
      stepSummary([
        row("tool_call", { toolCall: { id: "c", kind: "think", name: "compact", title: "Compact conversation" } }),
        row("tool_call", { toolCall: { id: "t", kind: "think", name: "Agent" } }),
      ]),
    ).toBe("Compacted the conversation, ran subagents");
  });
});

describe("stepLabel", () => {
  // The titles claude-agent-acp writes (measured on 0.74, unchanged on 0.78), verbatim in shape.
  it.each([
    [{ kind: "read", title: "Read internal/agenthub/hook.go" }, "Read", "hook.go"],
    [{ kind: "read", title: "Read docs/agent-hub.mdx (1 - 50)" }, "Read", "agent-hub.mdx"],
    [{ kind: "edit", title: "Edit frontend/src/turns.ts" }, "Edited", "turns.ts"],
    [{ kind: "edit", title: "Write notes.md" }, "Wrote", "notes.md"],
    [{ kind: "fetch", title: "Fetch https://code.claude.com/docs/en/hooks" }, "Fetched", "https://code.claude.com/docs/en/hooks"],
    [{ kind: "fetch", title: 'Search "claude code hooks"' }, "Searched the web for", "claude code hooks"],
    [{ kind: "search", title: 'grep -n "PreToolUse" internal' }, "Searched for", '-n "PreToolUse" internal'],
    [{ kind: "execute", title: "go test ./internal/agenthub/" }, "Ran", "go test ./internal/agenthub/"],
    [{ kind: "other", title: "mcp__devkit__db_listProfiles" }, "Db list profiles", "devkit"],
    [{ kind: "other", title: "mcp__devkit__tasks_transition" }, "Tasks transition", "devkit"],
    [{ kind: "execute", title: "cd /work/kong-plugin && pongo run" }, "Ran", "pongo run"],
    [{ kind: "execute", title: "cd /work/kong-plugin\ndocker info" }, "Ran", "docker info"],
    [
      { kind: "execute", title: "cd /work\ndocker info", rawInput: { command: "cd /work\ndocker info", description: "Check docker" } },
      "Check docker",
      "",
    ],
  ])("%o reads as a verb and its object", (call, verb, object) => {
    expect(stepLabel({ id: "x", ...call })).toEqual({ verb, object });
  });
});
