import { describe, expect, it } from "bun:test";

import { applySessionUpdate, createInitialTranscriptModel, initTranscript } from "./transcript";
import { runningSubagents, workSegments } from "./turns";

// What the pane of a subagent gets for each kind a subagent can emit, from the
// rows as the Go recorder stores them (SessionUpdate JSON, origin included) and
// from the live updates of the same turn. The pane reads
// runningSubagents(items).activity[id], so a row that does not carry the
// subagent's origin is not in it.
const SUB = "a1";
const at = "2026-10-29T00:00:00Z";
const stored = (seq: number, kind: string, payload: Record<string, unknown>, extra: Record<string, unknown> = {}) => ({
  ID: `r${seq}`,
  Seq: seq,
  Kind: kind,
  Payload: payload,
  CreatedAt: at,
  UpdatedAt: at,
  ...extra,
});

const wire = [
  { kind: "user_message_chunk", messageId: "u1", content: { type: "text", text: "go" } },
  { kind: "subagent_spawned", subagent: { id: SUB, name: "Explore", task: "look around" } },
  { kind: "plan", plan: [{ content: "lead step", status: "pending" }] },
  { kind: "agent_thought_chunk", origin: SUB, messageId: "t1", content: { type: "text", text: "hmm" } },
  { kind: "tool_call", origin: SUB, toolCall: { id: "c1", title: "ls", kind: "execute", status: "completed" } },
  { kind: "plan", origin: SUB, plan: [{ content: "its step", status: "in_progress" }] },
  { kind: "async_task_spawned", origin: SUB, asyncTask: { id: "bg1", name: "its build", state: "running" } },
  { kind: "async_task_state_update", origin: SUB, asyncTask: { id: "bg1", state: "completed" } },
  { kind: "subagent_spawned", origin: SUB, subagent: { id: "inner", name: "Inner", task: "deeper" } },
  { kind: "tool_call", origin: "inner", toolCall: { id: "c2", title: "pwd", kind: "execute", status: "completed" } },
  { kind: "agent_message_chunk", origin: SUB, messageId: "m1", content: { type: "text", text: "report" } },
] as const;

function reloaded() {
  return initTranscript(
    wire.map((update, index) => {
      const { kind, ...payload } = update as Record<string, unknown> & { kind: string };
      const text = (payload.content as { text?: string } | undefined)?.text ?? "";
      const call = payload.toolCall as { id?: string } | undefined;
      return stored(index + 1, kind, payload, { Text: text, ToolCallID: call?.id ?? "" });
    }),
  );
}

function live() {
  let model = createInitialTranscriptModel();
  for (const update of wire) model = applySessionUpdate(model, { ...update } as never);
  return model;
}

const label = (item: { kind: string; toolCall?: { id: string }; asyncTask?: { id: string }; subagent?: { id: string }; text?: string }) =>
  item.toolCall?.id ?? item.asyncTask?.id ?? item.subagent?.id ?? `${item.kind}:${item.text ?? ""}`;

describe.each([
  ["a page reloaded from storage", reloaded],
  ["the live stream", live],
])("a subagent's events from %s", (_name, build) => {
  it("lands every kind it emits in its own activity, in order", () => {
    const { activity } = runningSubagents(build().items);
    expect(activity[SUB].map(label)).toEqual([
      "agent_thought_chunk:hmm",
      "c1",
      "plan:",
      "bg1",
      "inner",
      "agent_message_chunk:report",
    ]);
  });

  it("keeps a nested subagent's steps with the nested one", () => {
    const { activity } = runningSubagents(build().items);
    expect(activity.inner.map(label)).toEqual(["c2"]);
  });

  it("leaves the lead agent's plan to the lead agent", () => {
    const plans = build().items.filter((item) => item.kind === "plan");
    expect(plans.map((item) => [item.origin, item.plan?.[0]?.content])).toEqual([
      [undefined, "lead step"],
      [SUB, "its step"],
    ]);
  });

  it("draws its plan and its steps as the pane's segments", () => {
    const { activity } = runningSubagents(build().items);
    expect(workSegments(activity[SUB]).map((segment) => segment.kind)).toEqual(["steps", "plan", "steps", "text"]);
  });
});

describe.each([
  ["a page reloaded from storage", (updates: Record<string, unknown>[]) =>
    initTranscript(updates.map(({ kind, ...payload }, index) => stored(index + 1, String(kind), payload)))],
  ["the live stream", (updates: Record<string, unknown>[]) => {
    let model = createInitialTranscriptModel();
    for (const update of updates) model = applySessionUpdate(model, { ...update } as never);
    return model;
  }],
])("plans of the lead agent and two subagents from %s", (_name, build) => {
  const plan = (content: string, origin?: string) => ({
    kind: "plan",
    ...(origin ? { origin } : {}),
    plan: [{ content, status: "pending" }],
  });

  it("keeps one plan per origin: an update replaces its own, never another's", () => {
    const model = build([
      { kind: "user_message_chunk", messageId: "u1", content: { type: "text", text: "go" } },
      plan("lead 1"),
      plan("A 1", "a"),
      plan("lead 2"),
      plan("B 1", "b"),
      plan("A 2", "a"),
    ]);
    const plans = model.items.filter((item) => item.kind === "plan");
    expect(plans.map((item) => [item.origin, item.plan?.[0]?.content])).toEqual([
      [undefined, "lead 2"],
      ["a", "A 2"],
      ["b", "B 1"],
    ]);
  });

  it("starts the lead's plan over with the next prompt, and a subagent's brief does not end it", () => {
    const model = build([
      { kind: "user_message_chunk", messageId: "u1", content: { type: "text", text: "go" } },
      plan("lead 1"),
      { kind: "user_message_chunk", origin: "a", messageId: "brief", content: { type: "text", text: "the brief" } },
      plan("lead 2"),
      { kind: "user_message_chunk", messageId: "u2", content: { type: "text", text: "again" } },
      plan("lead 3"),
    ]);
    expect(model.items.filter((item) => item.kind === "plan").map((item) => item.plan?.[0]?.content)).toEqual(["lead 2", "lead 3"]);
  });
});
