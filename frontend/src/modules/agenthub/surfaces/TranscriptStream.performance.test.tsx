import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { TranscriptItem } from "../types";
import {
  isTranscriptAtBottom,
  keepsFollowing,
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

  it("keeps following through 500 flushes that each outgrow the bottom tolerance", () => {
    const viewportHeight = 480;
    let scrollHeight = 1_200;
    let scrollTop = transcriptBottomScrollTop(scrollHeight, viewportHeight);
    let following = true;

    for (let flush = 0; flush < 500; flush += 1) {
      // A flush lands before the auto-scroll frame, and a scroll sample (the
      // previous auto-scroll's own event) reads the grown height.
      scrollHeight += 120 + (flush % 5);
      following = keepsFollowing(following, {
        atBottom: isTranscriptAtBottom(scrollHeight, scrollTop, viewportHeight),
        scrollTop,
        previousScrollTop: scrollTop,
        userScrolled: false,
      });
      expect(following).toBe(true);
      scrollTop = transcriptBottomScrollTop(scrollHeight, viewportHeight);
    }
  });

  it("stops following only when the reader scrolls up", () => {
    const sample = { atBottom: false, scrollTop: 1_000, previousScrollTop: 1_000, userScrolled: true };
    // A wheel down at the bottom moves nothing while a flush grows the foot.
    expect(keepsFollowing(true, sample)).toBe(true);
    // Wheel, touch, keyboard or scrollbar up.
    expect(keepsFollowing(true, { ...sample, scrollTop: 700 })).toBe(false);
    // Content shrinking under a follower clamps scrollTop without anyone scrolling.
    expect(keepsFollowing(true, { ...sample, scrollTop: 700, userScrolled: false })).toBe(true);
    // Back at the bottom, following resumes however it got there.
    expect(keepsFollowing(false, { ...sample, atBottom: true })).toBe(true);
    expect(keepsFollowing(false, { ...sample, scrollTop: 1_200 })).toBe(false);
  });
});
