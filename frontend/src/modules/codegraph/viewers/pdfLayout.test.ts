import { describe, expect, test } from "bun:test";
import {
  currentPage,
  dpiFor,
  fitWidthZoom,
  formatBytes,
  layoutPages,
  PAGE_GAP,
  PAGE_MARGIN,
  scrollAfterZoom,
  scrollTopOfPage,
  stepZoom,
  visibleRange,
} from "./pdfLayout";

// A4 and a landscape page, in points.
const A4: [number, number] = [595, 842];
const WIDE: [number, number] = [842, 595];

describe("layoutPages", () => {
  test("stacks pages with a gap and centres narrower ones", () => {
    const { boxes, width, height } = layoutPages([A4, WIDE, A4], 1);
    expect(boxes.map((b) => b.page)).toEqual([1, 2, 3]);
    expect(boxes[0].top).toBe(PAGE_MARGIN);
    expect(boxes[1].top).toBe(boxes[0].top + boxes[0].height + PAGE_GAP);
    expect(width).toBe(boxes[1].width);
    expect(boxes[1].left).toBe(0);
    expect(boxes[0].left).toBe(Math.round((width - boxes[0].width) / 2));
    expect(height).toBe(boxes[2].top + boxes[2].height + PAGE_MARGIN);
  });

  test("zoom scales every page and the whole column", () => {
    const one = layoutPages([A4, A4], 1);
    const two = layoutPages([A4, A4], 2);
    // Each box is rounded on its own, so doubling can differ by a pixel.
    expect(Math.abs(two.boxes[0].width - one.boxes[0].width * 2)).toBeLessThanOrEqual(1);
    expect(Math.abs(two.boxes[0].height - one.boxes[0].height * 2)).toBeLessThanOrEqual(1);
  });

  test("no pages, no height", () => {
    expect(layoutPages([], 1)).toEqual({ boxes: [], width: 0, height: 0 });
  });
});

describe("what is on screen", () => {
  const { boxes } = layoutPages(Array.from({ length: 10 }, () => A4), 1);
  const pageH = boxes[0].height + PAGE_GAP;

  test("the first screen shows the first pages plus one of overscan", () => {
    expect(visibleRange(boxes, 0, pageH * 1.5)).toEqual({ first: 1, last: 3 });
  });

  test("in the middle, the pages touching the viewport widened both ways", () => {
    const top = boxes[4].top;
    expect(visibleRange(boxes, top, boxes[4].height)).toEqual({ first: 4, last: 6 });
    expect(visibleRange(boxes, top, boxes[4].height, 0)).toEqual({ first: 5, last: 5 });
  });

  test("past the end it clamps to the last page", () => {
    expect(visibleRange(boxes, 1e9, 500)).toEqual({ first: 9, last: 10 });
    expect(visibleRange([], 0, 500)).toBeNull();
  });

  test("the current page is the one under the middle of the viewport", () => {
    expect(currentPage(boxes, 0, 600)).toBe(1);
    expect(currentPage(boxes, scrollTopOfPage(boxes, 4), 600)).toBe(4);
    expect(currentPage(boxes, 1e9, 600)).toBe(10);
    expect(currentPage([], 0, 600)).toBe(0);
  });

  test("scrollTopOfPage leaves the margin above the page and clamps unknown pages", () => {
    expect(scrollTopOfPage(boxes, 1)).toBe(0);
    expect(scrollTopOfPage(boxes, 3)).toBe(boxes[2].top - PAGE_MARGIN);
    expect(scrollTopOfPage(boxes, 99)).toBe(0);
  });
});

describe("zoom", () => {
  test("steps through the table and stops at its ends", () => {
    expect(stepZoom(1, 1)).toBe(1.25);
    expect(stepZoom(1, -1)).toBe(0.75);
    expect(stepZoom(3, 1)).toBe(3);
    expect(stepZoom(0.5, -1)).toBe(0.5);
  });

  test("from a fit-width value it goes to the next step that way", () => {
    expect(stepZoom(1.1, 1)).toBe(1.25);
    expect(stepZoom(1.1, -1)).toBe(1);
  });

  test("fit width fills the space with the widest page, within the limits", () => {
    expect(fitWidthZoom([A4, WIDE], 842 * (96 / 72))).toBe(1);
    expect(fitWidthZoom([A4], 100)).toBe(0.5);
    expect(fitWidthZoom([A4], 100000)).toBe(3);
    expect(fitWidthZoom([], 800)).toBeGreaterThan(0);
  });

  test("a zoom change keeps the reader's place", () => {
    expect(scrollAfterZoom(1000, 1, 2)).toBe(2000);
    expect(scrollAfterZoom(1000, 2, 1)).toBe(500);
    expect(scrollAfterZoom(1000, 0, 1)).toBe(1000);
  });
});

describe("dpiFor", () => {
  test("rounds up to a level so nearby zooms share a render", () => {
    expect(dpiFor(0.5)).toBe(72);
    expect(dpiFor(1)).toBe(96);
    expect(dpiFor(1.25)).toBe(120);
    expect(dpiFor(1.5)).toBe(150);
  });

  test("a denser display asks for more, up to Go's ceiling", () => {
    expect(dpiFor(1, 2)).toBe(192);
    expect(dpiFor(3, 2)).toBe(300);
    expect(dpiFor(1, 0.5)).toBe(96);
  });
});

test("formatBytes", () => {
  expect(formatBytes(512)).toBe("512 B");
  expect(formatBytes(2048)).toBe("2.0 KB");
  expect(formatBytes(3 * 1024 * 1024)).toBe("3.0 MB");
  expect(formatBytes(40 * 1024 * 1024)).toBe("40 MB");
});
