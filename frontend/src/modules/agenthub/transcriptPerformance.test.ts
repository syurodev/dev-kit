import { describe, expect, it } from "bun:test";
import { UpdateKind } from "../../../bindings/devkit/internal/agenthub/models";
import { applySessionUpdate, createInitialTranscriptModel, initTranscript } from "./transcript";
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
    const applySamples: number[] = [];
    for (let run = 0; run < 3; run += 1) {
      let model = createInitialTranscriptModel();
      const started = performance.now();
      for (const update of updates) {
        model = applySessionUpdate(model, update);
      }
      applySamples.push(performance.now() - started);
      expect(model.items).toHaveLength(1);
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
        `applyMsMedian=${median(applySamples).toFixed(2)} ` +
        `initRows=${entries.length} initMsMedian=${median(initSamples).toFixed(2)}`,
    );
  });
});
