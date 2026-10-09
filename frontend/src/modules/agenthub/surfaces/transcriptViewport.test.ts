import { describe, expect, it } from "bun:test";
import { scrollTopAfterViewportResize } from "./TranscriptStream";

describe("the transcript when the composer under it changes height", () => {
  it("keeps following the end: the newest line stays in view as the viewport shrinks or grows", () => {
    // 3000 of content; the viewport goes from 500 to 380 (a panel opened).
    expect(scrollTopAfterViewportResize(true, 500, 380, 3000)).toBe(2620);
    // ...and back.
    expect(scrollTopAfterViewportResize(true, 380, 500, 3000)).toBe(2500);
  });

  it("leaves a reader alone: nothing moves while they are reading history", () => {
    expect(scrollTopAfterViewportResize(false, 500, 150, 3000)).toBeNull();
    expect(scrollTopAfterViewportResize(false, 150, 500, 3000)).toBeNull();
  });

  it("does nothing when the height did not change (a width-only resize, an observer's first call)", () => {
    expect(scrollTopAfterViewportResize(true, 500, 500, 3000)).toBeNull();
  });

  it("never asks for a negative position when the content is shorter than the viewport", () => {
    expect(scrollTopAfterViewportResize(true, 500, 400, 120)).toBe(0);
  });
});
