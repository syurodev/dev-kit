import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { TranscriptItem } from "../types";
import {
  TRANSCRIPT_OVERSCAN,
  TRANSCRIPT_VIRTUALIZE_THRESHOLD,
  TranscriptStream,
} from "./TranscriptStream";

function largeTurnFixture(turnCount: number): TranscriptItem[] {
  return Array.from({ length: turnCount }, (_, index) => ({
    id: `prompt-${index}`,
    kind: "user_message_chunk",
    text: `Prompt ${index}`,
  }));
}

describe("TranscriptStream long-history budget", () => {
  it("keeps a 5,000-turn transcript out of the linear DOM path", () => {
    const items = largeTurnFixture(5_000);
    const started = performance.now();
    const html = renderToString(<TranscriptStream items={items} streamKey="perf" />);
    const renderMs = performance.now() - started;
    const sectionCount = (html.match(/data-testid="agent-turn"/g) ?? []).length;

    console.log(
      `[agenthub viewport before/after] turns=${items.length} ` +
        `legacySectionCount=${items.length} ` +
        `virtualSectionCount=${sectionCount} ` +
        `virtualDomBudget=${TRANSCRIPT_OVERSCAN * 2 + 20} renderMs=${renderMs.toFixed(2)}`,
    );
    expect(items.length).toBeGreaterThan(TRANSCRIPT_VIRTUALIZE_THRESHOLD);
    expect(sectionCount).toBeLessThanOrEqual(TRANSCRIPT_OVERSCAN * 2 + 20);
    expect(html).not.toContain("scroll-smooth");
  });
});
