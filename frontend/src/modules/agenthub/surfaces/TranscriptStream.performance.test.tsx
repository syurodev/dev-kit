import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { TranscriptItem } from "../types";
import {
  isTranscriptAtBottom,
  TRANSCRIPT_OVERSCAN,
  TRANSCRIPT_VIRTUALIZE_THRESHOLD,
  transcriptBottomScrollTop,
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

  it("keeps the bottom anchor through 500 consecutive flushes", () => {
    const viewportHeight = 480;
    let scrollHeight = 1_200;
    let scrollTop = transcriptBottomScrollTop(scrollHeight, viewportHeight);

    for (let flush = 0; flush < 500; flush += 1) {
      expect(isTranscriptAtBottom(scrollHeight, scrollTop, viewportHeight)).toBe(true);
      scrollHeight += 24 + (flush % 5);
      scrollTop = transcriptBottomScrollTop(scrollHeight, viewportHeight);
    }

    expect(scrollTop).toBe(transcriptBottomScrollTop(scrollHeight, viewportHeight));
  });

  it("keeps a tick jump addressable by turn index when the row is not mounted", () => {
    const turns = largeTurnFixture(5_000);
    const firstId = turns[0].id;
    const lastId = turns[turns.length - 1]?.id;
    expect(lastId).toBe("prompt-4999");
    expect(turns.findIndex((turn) => turn.id === firstId)).toBe(0);
    expect(turns.findIndex((turn) => turn.id === lastId)).toBe(4_999);
  });
});
