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
import { largeTranscriptEntries, LARGE_TRANSCRIPT_ROW_COUNT } from "./transcriptPerformance.fixture";

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
