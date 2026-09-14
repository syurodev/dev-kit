import { UpdateKind } from "../../../bindings/devkit/internal/agenthub/models";

const TURN_COUNT = 5_000;
const ROWS_PER_TURN = 10;

/**
 * A repeatable transcript workload for the realtime path. It is generated so
 * the test does not carry a multi-megabyte JSON blob, but it still exercises
 * the shapes that make a transcript expensive: text, thought, tool output and
 * plan rows across 5,000 turns.
 */
export function largeTranscriptEntries(): Record<string, unknown>[] {
  const entries: Record<string, unknown>[] = [];
  for (let turn = 0; turn < TURN_COUNT; turn += 1) {
    const stamp = `2026-09-14T00:${String(Math.floor(turn / 60) % 60).padStart(2, "0")}:${String(turn % 60).padStart(2, "0")}Z`;
    const toolCallId = `tool-${turn}`;
    const payload = (value: Record<string, unknown>) => JSON.stringify(value);
    entries.push(
      {
        ID: `user-${turn}`,
        Kind: UpdateKind.UpdateUserMessageChunk,
        Text: `Inspect service ${turn}`,
        Payload: payload({ messageId: `user-${turn}` }),
        CreatedAt: stamp,
        UpdatedAt: stamp,
      },
      {
        ID: `thought-${turn}`,
        Kind: UpdateKind.UpdateAgentThoughtChunk,
        Text: `Reasoning about turn ${turn}`,
        Payload: payload({ messageId: `thought-${turn}` }),
        CreatedAt: stamp,
        UpdatedAt: stamp,
      },
      {
        ID: toolCallId,
        Kind: UpdateKind.UpdateToolCall,
        ToolCallID: toolCallId,
        Payload: payload({
          toolCall: {
            id: toolCallId,
            title: `Read service ${turn}`,
            kind: "read",
            status: "in_progress",
            rawOutput: "tool output ".repeat(128),
          },
        }),
        CreatedAt: stamp,
        UpdatedAt: stamp,
      },
      {
        ID: `${toolCallId}-update`,
        Kind: UpdateKind.UpdateToolCallUpdate,
        ToolCallID: toolCallId,
        Payload: payload({
          toolCall: {
            id: toolCallId,
            status: "completed",
            rawOutput: "completed output ".repeat(128),
          },
        }),
        CreatedAt: stamp,
        UpdatedAt: stamp,
      },
      {
        ID: `plan-${turn}`,
        Kind: UpdateKind.UpdatePlan,
        Payload: payload({
          plan: [
            { content: `Check ${turn}`, priority: "high", status: "completed" },
            { content: `Update ${turn}`, priority: "medium", status: "in_progress" },
          ],
        }),
        CreatedAt: stamp,
        UpdatedAt: stamp,
      },
      {
        ID: `agent-${turn}-a`,
        Kind: UpdateKind.UpdateAgentMessageChunk,
        Text: `Found the first result for ${turn}.`,
        Payload: payload({ messageId: `agent-${turn}-a` }),
        CreatedAt: stamp,
        UpdatedAt: stamp,
      },
      {
        ID: `agent-${turn}-b`,
        Kind: UpdateKind.UpdateAgentMessageChunk,
        Text: `The second result for ${turn}.`,
        Payload: payload({ messageId: `agent-${turn}-b` }),
        CreatedAt: stamp,
        UpdatedAt: stamp,
      },
      {
        ID: `thought-${turn}-2`,
        Kind: UpdateKind.UpdateAgentThoughtChunk,
        Text: `Verifying turn ${turn}`,
        Payload: payload({ messageId: `thought-${turn}-2` }),
        CreatedAt: stamp,
        UpdatedAt: stamp,
      },
      {
        ID: `agent-${turn}-final`,
        Kind: UpdateKind.UpdateAgentMessageChunk,
        Text: `Completed turn ${turn}.`,
        Payload: payload({ messageId: `agent-${turn}-final` }),
        CreatedAt: stamp,
        UpdatedAt: stamp,
      },
      {
        ID: `unknown-${turn}`,
        Kind: UpdateKind.UpdateUnknown,
        Payload: payload({ kind: "future_vendor_update", turn, rawOutput: "x".repeat(64) }),
        CreatedAt: stamp,
        UpdatedAt: stamp,
      },
    );
  }
  return entries;
}

export const LARGE_TRANSCRIPT_ROW_COUNT = TURN_COUNT * ROWS_PER_TURN;

/** A repeatable 20 KiB answer for the streaming markdown parser benchmark. */
export function largeMarkdownAnswer(): string {
  const block = [
    "## Result",
    "",
    "The agent compared the current implementation with the target behaviour.",
    "",
    "| Check | Status |",
    "| --- | :---: |",
    "| identity | passed |",
    "| publish | passed |",
    "",
    "- Keep completed blocks stable.",
    "- Parse only the active tail.",
    "",
    "```ts",
    "const result = await measureStream();",
    "return result.identityStable;",
    "```",
    "",
  ].join("\n");
  let answer = "";
  while (answer.length < 20_000) answer += block;
  return answer.slice(0, 20_000);
}
