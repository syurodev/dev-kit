import { describe, expect, it } from "bun:test";
import { UpdateKind } from "../../../bindings/devkit/internal/agenthub/models";
import {
  applySessionUpdate,
  createInitialTranscriptModel,
  initTranscript,
  isImmediateTranscriptUpdate,
  TranscriptEventBuffer,
  type TranscriptModel,
} from "./transcript";
import { groupTurns } from "./turns";
import { largeMarkdownAnswer, largeTranscriptEntries, LARGE_TRANSCRIPT_ROW_COUNT } from "./transcriptPerformance.fixture";
import { parseMarkdown, parseMarkdownIncremental } from "./transcript/agentMarkdown";
import type { TranscriptItem } from "./types";

const TOKEN_COUNT = 1_000;

function tokenUpdates() {
  return Array.from({ length: TOKEN_COUNT }, (_, index) => ({
    kind: UpdateKind.UpdateAgentMessageChunk,
    content: { type: "text", text: `token-${index} ` },
    messageId: "perf-message",
  }));
}

function median(samples: number[]): number {
  const sorted = [...samples].sort((a, b) => a - b);
  return sorted[Math.floor(sorted.length / 2)];
}

/** Regrouping 50,000 rows per token is the cost being measured, so a short stream. */
const STREAMED_TOKENS = 20;

/**
 * Streams tokens into the fixture's last answer, regrouping after each one
 * (from scratch, or from the previous grouping), and counts the Turn objects
 * that actually changed identity.
 */
function streamIntoLastAnswer(rows: TranscriptItem[], tokens: number, sharePrevious: boolean) {
  const lastAnswerIndex = rows.length - 2;
  let previous = groupTurns(rows);
  let changed = 0;
  let groupMs = 0;
  for (let token = 0; token < tokens; token += 1) {
    const answer = rows[lastAnswerIndex];
    rows = [
      ...rows.slice(0, lastAnswerIndex),
      { ...answer, text: `${answer.text ?? ""} token-${token} ` },
      ...rows.slice(lastAnswerIndex + 1),
    ];
    const started = performance.now();
    const next = groupTurns(rows, sharePrevious ? previous : undefined);
    groupMs += performance.now() - started;
    for (let index = 0; index < next.length; index += 1) {
      if (next[index] !== previous[index]) changed += 1;
    }
    previous = next;
  }
  return { turns: previous.length, changed, groupMs: groupMs / tokens };
}

describe("Agent Hub transcript performance fixture", () => {
  it("generates the workload it measures", () => {
    expect(largeTranscriptEntries()).toHaveLength(LARGE_TRANSCRIPT_ROW_COUNT);
  });

  it("prints repeatable before/after measurements for the same fixture", () => {
    const updates = tokenUpdates();
    const legacyApplySamples: number[] = [];
    for (let run = 0; run < 3; run += 1) {
      let model = createInitialTranscriptModel();
      const started = performance.now();
      for (const update of updates) {
        model = applySessionUpdate(model, update);
      }
      legacyApplySamples.push(performance.now() - started);
      expect(model.items).toHaveLength(1);
    }

    const batchedApplySamples: number[] = [];
    for (let run = 0; run < 3; run += 1) {
      const frames: Array<() => void> = [];
      const buffer = new TranscriptEventBuffer(
        createInitialTranscriptModel(),
        () => undefined,
        (callback) => {
          frames.push(callback);
          return () => undefined;
        },
      );
      for (const update of updates) buffer.enqueue(update);
      const started = performance.now();
      frames[0]();
      batchedApplySamples.push(performance.now() - started);
    }

    const entries = largeTranscriptEntries();
    const initSamples: number[] = [];
    for (let run = 0; run < 3; run += 1) {
      const started = performance.now();
      const model = initTranscript(entries);
      initSamples.push(performance.now() - started);
      expect(model.items.length).toBeGreaterThan(0);
    }

    console.log(
      `[agenthub perf] tokens=${TOKEN_COUNT} ` +
        `legacyApplyMsMedian=${median(legacyApplySamples).toFixed(2)} ` +
        `batchedApplyMsMedian=${median(batchedApplySamples).toFixed(2)} ` +
        `initRows=${entries.length} initMsMedian=${median(initSamples).toFixed(2)}`,
    );
  });

  it("prints the pre-structural-sharing render and markdown baselines", () => {
    const entries = largeTranscriptEntries();
    const rows: TranscriptItem[] = entries.map((entry) => ({
      id: String(entry.ID),
      kind: String(entry.Kind),
      text: typeof entry.Text === "string" ? entry.Text : undefined,
      raw: entry.Payload,
      createdAt: String(entry.CreatedAt),
      updatedAt: String(entry.UpdatedAt),
      toolCallId: typeof entry.ToolCallID === "string" ? entry.ToolCallID : undefined,
    }));
    const { turns, changed: changedTurnCount, groupMs } = streamIntoLastAnswer(rows, STREAMED_TOKENS, false);

    const markdownAnswer = largeMarkdownAnswer();
    const markdownSamples: number[] = [];
    for (let run = 0; run < 1; run += 1) {
      const started = performance.now();
      for (let offset = 20; offset <= markdownAnswer.length; offset += 20) parseMarkdown(markdownAnswer.slice(0, offset));
      markdownSamples.push(performance.now() - started);
    }

    console.log(
        `[agenthub perf before] turns=${turns} tokens=${STREAMED_TOKENS} ` +
        `agentTurnIdentityChanges=${changedTurnCount} ` +
        `groupTurnsOneTokenMs=${groupMs.toFixed(2)} ` +
        `markdownBytes=${markdownAnswer.length} markdownMs=${median(markdownSamples).toFixed(2)}`,
    );
    // Regrouped from scratch, every Turn is a new object on every token.
    expect(changedTurnCount).toBe(turns * STREAMED_TOKENS);
  }, 30_000);

  it("prints the post-structural-sharing render and markdown measurements", () => {
    const entries = largeTranscriptEntries();
    const rows: TranscriptItem[] = entries.map((entry) => ({
      id: String(entry.ID),
      kind: String(entry.Kind),
      text: typeof entry.Text === "string" ? entry.Text : undefined,
      raw: entry.Payload,
      createdAt: String(entry.CreatedAt),
      updatedAt: String(entry.UpdatedAt),
      toolCallId: typeof entry.ToolCallID === "string" ? entry.ToolCallID : undefined,
    }));
    const { turns, changed: changedTurnCount, groupMs } = streamIntoLastAnswer(rows, STREAMED_TOKENS, true);

    const markdownAnswer = largeMarkdownAnswer();
    const markdownSamples: number[] = [];
    let finalState;
    for (let run = 0; run < 1; run += 1) {
      let state;
      const started = performance.now();
      for (let offset = 20; offset <= markdownAnswer.length; offset += 20) {
        state = parseMarkdownIncremental(markdownAnswer.slice(0, offset), state);
      }
      markdownSamples.push(performance.now() - started);
      finalState = state;
    }
    expect(finalState?.blocks).toEqual(parseMarkdown(markdownAnswer));
    console.log(
        `[agenthub perf after] turns=${turns} tokens=${STREAMED_TOKENS} ` +
        `agentTurnIdentityChanges=${changedTurnCount} ` +
        `groupTurnsOneTokenMs=${groupMs.toFixed(2)} ` +
        `markdownBytes=${markdownAnswer.length} markdownMs=${median(markdownSamples).toFixed(2)}`,
    );
    // Only the turn each token landed in.
    expect(changedTurnCount).toBe(STREAMED_TOKENS);
  }, 30_000);

  it("coalesces one stream into one publish and keeps control events immediate", () => {
    const frames: Array<() => void> = [];
    const published: TranscriptModel[] = [];
    const buffer = new TranscriptEventBuffer(
      createInitialTranscriptModel(),
      (model) => published.push(model),
      (callback) => {
        frames.push(callback);
        return () => undefined;
      },
    );

    for (const update of tokenUpdates()) buffer.enqueue(update);
    expect(published).toHaveLength(0);
    expect(frames).toHaveLength(1);
    frames[0]();
    expect(published).toHaveLength(1);
    expect(published[0].items[0].text).toContain("token-999");

    buffer.enqueue(tokenUpdates()[0]);
    const usage = {
      kind: UpdateKind.UpdateUsage,
      usage: { used: 1, size: 100, amount: 0 },
    };
    expect(isImmediateTranscriptUpdate(usage)).toBe(true);
    buffer.applyImmediate(usage);
    // The control-plane path publishes synchronously; permission events use
    // the same dedicated, non-buffered path in AgentSessionsSubscriber.
    expect(published).toHaveLength(2);
    expect(published[1].usage?.used).toBe(1);
  });

});
