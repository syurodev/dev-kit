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

describe("Agent Hub transcript performance fixture", () => {
  it("keeps the workload and legacy publish count explicit", () => {
    expect(largeTranscriptEntries()).toHaveLength(LARGE_TRANSCRIPT_ROW_COUNT);
    // Before the shared shell model, AgentHubView and AgentCompanion each
    // owned a hook, so every token caused one publish in each hook.
    expect(TOKEN_COUNT * 2).toBe(2_000);
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
      `[agenthub perf] tokens=${TOKEN_COUNT} legacyPublishes=${TOKEN_COUNT * 2} ` +
        `batchedPublishes=1 legacyApplyMsMedian=${median(legacyApplySamples).toFixed(2)} ` +
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
    const lastAnswerIndex = rows.length - 2;
    let previous = groupTurns(rows);
    let changedTurnCount = 0;
    const turnSamples: number[] = [];
    for (let run = 0; run < 1; run += 1) {
      let currentRows = rows;
      let currentTurns = previous;
      const started = performance.now();
      let changes = 0;
      for (let token = 0; token < TOKEN_COUNT; token += 1) {
        const answer = currentRows[lastAnswerIndex];
        const nextAnswer = { ...answer, text: `${answer.text ?? ""} token-${token} ` };
        currentRows = [...currentRows.slice(0, lastAnswerIndex), nextAnswer, ...currentRows.slice(lastAnswerIndex + 1)];
        const nextTurns = groupTurns(currentRows);
        // groupTurns without a previous result creates every Turn object.
        changes += nextTurns.length;
        currentTurns = nextTurns;
      }
      turnSamples.push(performance.now() - started);
      changedTurnCount = changes;
      previous = currentTurns;
    }

    const answer = largeMarkdownAnswer();
    const markdownSamples: number[] = [];
    for (let run = 0; run < 1; run += 1) {
      const started = performance.now();
      for (let offset = 20; offset <= answer.length; offset += 20) parseMarkdown(answer.slice(0, offset));
      markdownSamples.push(performance.now() - started);
    }

    console.log(
      `[agenthub perf before] turns=5000 tokens=${TOKEN_COUNT} ` +
        `agentTurnIdentityChanges=${changedTurnCount} ` +
        `groupTurnsMsMedian=${median(turnSamples).toFixed(2)} ` +
        `markdownBytes=${answer.length} markdownMsMedian=${median(markdownSamples).toFixed(2)}`,
    );
    expect(changedTurnCount).toBe(5_000_000);
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
    const lastAnswerIndex = rows.length - 2;
    const turnSamples: number[] = [];
    let changedTurnCount = 0;
    for (let run = 0; run < 1; run += 1) {
      let currentRows = rows;
      let currentTurns = groupTurns(rows);
      const started = performance.now();
      let changes = 0;
      for (let token = 0; token < TOKEN_COUNT; token += 1) {
        const answer = currentRows[lastAnswerIndex];
        const nextAnswer = { ...answer, text: `${answer.text ?? ""} token-${token} ` };
        currentRows = [...currentRows.slice(0, lastAnswerIndex), nextAnswer, ...currentRows.slice(lastAnswerIndex + 1)];
        const nextTurns = groupTurns(currentRows, currentTurns);
        changes += nextTurns[nextTurns.length - 1] !== currentTurns[currentTurns.length - 1] ? 1 : 0;
        currentTurns = nextTurns;
      }
      turnSamples.push(performance.now() - started);
      changedTurnCount = changes;
    }

    const answer = largeMarkdownAnswer();
    const markdownSamples: number[] = [];
    let finalState;
    for (let run = 0; run < 1; run += 1) {
      let state;
      const started = performance.now();
      for (let offset = 20; offset <= answer.length; offset += 20) {
        state = parseMarkdownIncremental(answer.slice(0, offset), state);
      }
      markdownSamples.push(performance.now() - started);
      finalState = state;
    }
    expect(finalState?.blocks).toEqual(parseMarkdown(answer));
    console.log(
      `[agenthub perf after] turns=5000 tokens=${TOKEN_COUNT} ` +
        `agentTurnIdentityChanges=${changedTurnCount} ` +
        `groupTurnsMsMedian=${median(turnSamples).toFixed(2)} ` +
        `markdownBytes=${answer.length} markdownMsMedian=${median(markdownSamples).toFixed(2)}`,
    );
    expect(changedTurnCount).toBe(TOKEN_COUNT);
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

  it("keeps another session's flush from changing this model identity", () => {
    const publishedA: unknown[] = [];
    const publishedB: unknown[] = [];
    const framesA: Array<() => void> = [];
    const framesB: Array<() => void> = [];
    const a = new TranscriptEventBuffer(
      createInitialTranscriptModel(),
      (model) => publishedA.push(model),
      (cb) => {
        framesA.push(cb);
        return () => undefined;
      },
    );
    const b = new TranscriptEventBuffer(
      createInitialTranscriptModel(),
      (model) => publishedB.push(model),
      (cb) => {
        framesB.push(cb);
        return () => undefined;
      },
    );

    a.enqueue(tokenUpdates()[0]);
    b.enqueue(tokenUpdates()[1]);
    expect(publishedA).toHaveLength(0);
    framesB[0]();
    const modelB = publishedB[0];

    framesA[0]();
    const modelA = publishedA[0];
    expect(publishedB).toHaveLength(1);
    expect(modelA).not.toBe(modelB);
  });
});
