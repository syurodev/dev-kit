import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TranscriptTicks, ticksOf } from "./TranscriptTicks";
import { groupTurns } from "../turns";

describe("ticksOf", () => {
  it("makes one tick per prompt, with the answer as plain text", () => {
    const ticks = ticksOf([
      { id: "t0", answer: [{ text: "before anyone spoke" }] },
      {
        id: "t1",
        prompt: { text: "  explore\nthe repo " },
        answer: [{ text: "## Cấu trúc\n\n- `dev-kit-backend` — **Gradle**" }],
      },
      {
        id: "t3",
        prompt: { text: "table" },
        answer: [{ text: "```bash\n./gradlew build\n```\n| A | B |\n| --- | ---: |\n| x | 1 |" }],
      },
      { id: "t2", prompt: { text: "run tests" }, answer: [] },
    ]);
    expect(ticks).toEqual([
      { id: "t1", prompt: "explore the repo", answer: "Cấu trúc dev-kit-backend — Gradle" },
      { id: "t3", prompt: "table", answer: "./gradlew build A B x 1" },
      { id: "t2", prompt: "run tests", answer: undefined },
    ]);
  });

  it("retains ticks for turns whose identity did not change", () => {
    const rows = [
      { id: "prompt-1", kind: "user_message_chunk", text: "first" },
      { id: "answer-1", kind: "agent_message_chunk", text: "one" },
      { id: "prompt-2", kind: "user_message_chunk", text: "second" },
      { id: "answer-2", kind: "agent_message_chunk", text: "two" },
    ];
    const previousTurns = groupTurns(rows);
    const previousTicks = ticksOf(previousTurns);
    const currentTurns = groupTurns([...rows.slice(0, -1), { ...rows[3], text: "two more" }], previousTurns);
    const currentTicks = ticksOf(currentTurns, previousTurns, previousTicks);

    expect(currentTicks[0]).toBe(previousTicks[0]);
    expect(currentTicks[1]).not.toBe(previousTicks[1]);
  });

  // The stream observes its sections keyed on this array; a new one every
  // flush disconnected and re-observed every mounted section per token.
  it("returns the previous array when no tick changed", () => {
    const rows = [
      { id: "prompt-1", kind: "user_message_chunk", text: "first" },
      { id: "answer-1", kind: "agent_message_chunk", text: "one" },
      { id: "prompt-2", kind: "user_message_chunk", text: "second" },
      { id: "tool-2", kind: "tool_call", toolCall: { id: "t", status: "in_progress" } },
      { id: "answer-2", kind: "agent_message_chunk", text: "two" },
    ];
    const previousTurns = groupTurns(rows);
    const previousTicks = ticksOf(previousTurns);

    const regrouped = groupTurns([...rows], previousTurns);
    expect(ticksOf(regrouped, previousTurns, previousTicks)).toBe(previousTicks);

    // A tool call finishing changes the turn, not its prompt or answer preview.
    const toolDone = groupTurns(
      [...rows.slice(0, 3), { ...rows[3], toolCall: { id: "t", status: "completed" } }, rows[4]],
      previousTurns,
    );
    expect(toolDone[1]).not.toBe(previousTurns[1]);
    expect(ticksOf(toolDone, previousTurns, previousTicks)).toBe(previousTicks);
  });
});

describe("TranscriptTicks", () => {
  const ticks = [
    { id: "a", prompt: "first" },
    { id: "b", prompt: "second" },
  ];

  it("marks the prompt being read", () => {
    const html = renderToString(<TranscriptTicks ticks={ticks} active={1} onJump={() => {}} />);
    expect(html.match(/data-testid="transcript-tick"/g)).toHaveLength(2);
    expect(html).toContain('aria-label="second" aria-current="location"');
  });

  it("stays out of the way of a one-prompt conversation", () => {
    const html = renderToString(<TranscriptTicks ticks={ticks.slice(0, 1)} active={0} onJump={() => {}} />);
    expect(html).toBe("");
  });
});
