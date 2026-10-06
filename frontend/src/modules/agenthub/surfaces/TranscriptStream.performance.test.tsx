import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { TranscriptItem } from "../types";
import {
  estimateTurnSize,
  isTranscriptAtBottom,
  keepsFollowing,
  shouldVirtualizeTranscript,
  TRANSCRIPT_HEAVY_OVERSCAN,
  TRANSCRIPT_OVERSCAN,
  TRANSCRIPT_VIRTUALIZE_ITEMS,
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

  it("windows a session of few turns that are heavy, not only one of many turns", () => {
    // 47 prompts, each with 40 tool calls and 20 things said between them: the
    // shape of a long working session. Mounted whole it was ~58,000 DOM nodes.
    const items: TranscriptItem[] = [];
    for (let turn = 0; turn < 47; turn += 1) {
      items.push({ id: `u${turn}`, kind: "user_message_chunk", text: `Prompt ${turn}` });
      for (let step = 0; step < 40; step += 1) {
        items.push({ id: `t${turn}-${step}`, kind: "tool_call", toolCall: { id: `t${turn}-${step}`, title: "Read", status: "completed" } });
        if (step % 2 === 0) items.push({ id: `m${turn}-${step}`, kind: "agent_message_chunk", text: `Looking at ${step}` });
      }
    }
    expect(shouldVirtualizeTranscript(47, items.length)).toBe(true);
    const html = renderToString(<TranscriptStream items={items} streamKey="heavy" />);
    const sectionCount = (html.match(/data-testid="agent-turn"/g) ?? []).length;
    expect(sectionCount).toBeGreaterThan(0);
    expect(sectionCount).toBeLessThanOrEqual(TRANSCRIPT_HEAVY_OVERSCAN * 2 + 20);
    expect(sectionCount).toBeLessThan(47);
  });

  it("leaves a short conversation mounted whole", () => {
    expect(shouldVirtualizeTranscript(10, TRANSCRIPT_VIRTUALIZE_ITEMS)).toBe(false);
    expect(shouldVirtualizeTranscript(10, TRANSCRIPT_VIRTUALIZE_ITEMS + 1)).toBe(true);
    expect(shouldVirtualizeTranscript(TRANSCRIPT_VIRTUALIZE_THRESHOLD + 1, 10)).toBe(true);
  });

  it("estimates a heavy turn taller than a bare prompt, and never absurdly", () => {
    const row = (id: string, text?: string): TranscriptItem => ({ id, kind: "agent_message_chunk", text });
    const bare = estimateTurnSize({ id: "a", work: [], answer: [], notes: [], subagents: {} });
    const heavy = estimateTurnSize({
      id: "b",
      work: Array.from({ length: 60 }, (_, i) => row(`w${i}`, i % 2 ? "said" : undefined)),
      answer: [row("x", "y".repeat(2_000))],
      notes: [],
      subagents: {},
    });
    expect(heavy).toBeGreaterThan(bare);
    const huge = estimateTurnSize({ id: "c", work: Array.from({ length: 5_000 }, (_, i) => row(`w${i}`, "t")), answer: [], notes: [], subagents: {} });
    expect(huge).toBeLessThanOrEqual(4_000);
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
