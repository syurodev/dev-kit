import { describe, expect, it } from "bun:test";
import {
  placeInspector,
  EDGE_MARGIN,
  DOCK_TOP_RESERVE,
  MIN_ANCHORED_WIDTH,
  PREFERRED_HEIGHT,
  PREFERRED_WIDTH,
  type Rect,
} from "./placeInspector";

// A node roughly the size ApiNode renders at 1x zoom.
const node = (over: Partial<Rect> = {}): Rect => ({
  x: 400,
  y: 300,
  width: 268,
  height: 120,
  ...over,
});

const BIG = { width: 1600, height: 1000 };

describe("placeInspector", () => {
  it("anchors right at full size when the canvas is roomy", () => {
    const p = placeInspector(node(), BIG);
    expect(p.mode).toBe("anchored");
    if (p.mode !== "anchored") return;
    expect(p.side).toBe("right");
    expect(p.width).toBe(PREFERRED_WIDTH);
    expect(p.height).toBe(PREFERRED_HEIGHT);
    expect(p.align).toBe("center");
  });

  it("flips to the left when the node sits near the right edge", () => {
    // Right room: 1600 - (1250+268) - 16 - 12 = 54. Left room: 1250 - 28 = 1222.
    const p = placeInspector(node({ x: 1250 }), BIG);
    expect(p.mode).toBe("anchored");
    if (p.mode !== "anchored") return;
    expect(p.side).toBe("left");
    expect(p.width).toBe(PREFERRED_WIDTH);
  });

  it("docks when neither side can hold a readable panel", () => {
    // 700px canvas, node centred: each side has ~188px, under MIN_ANCHORED_WIDTH.
    const p = placeInspector(node({ x: 216 }), { width: 700, height: 800 });
    expect(p.mode).toBe("docked");
  });

  it("never renders wider than the canvas allows", () => {
    const canvas = { width: 360, height: 400 };
    const p = placeInspector(node({ x: 40 }), canvas);
    expect(p.width).toBeLessThanOrEqual(canvas.width - EDGE_MARGIN * 2);
    expect(p.height).toBeLessThanOrEqual(canvas.height - EDGE_MARGIN * 2);
  });

  it("shrinks to the room actually available rather than overflowing", () => {
    // Right room = 1000 - (300+268) - 28 = 404 -> wide enough to anchor, but
    // narrower than the preferred 420.
    const p = placeInspector(node({ x: 300 }), { width: 1000, height: 900 });
    expect(p.mode).toBe("anchored");
    if (p.mode !== "anchored") return;
    expect(p.side).toBe("right");
    expect(p.width).toBe(404);
    expect(p.width).toBeGreaterThanOrEqual(MIN_ANCHORED_WIDTH);
  });

  it("aligns to the node's top when centring would clip above the canvas", () => {
    const p = placeInspector(node({ y: 20 }), BIG);
    expect(p.mode).toBe("anchored");
    if (p.mode !== "anchored") return;
    expect(p.align).toBe("start");
  });

  it("aligns to the node's bottom when centring would clip below the canvas", () => {
    const p = placeInspector(node({ y: 900 }), BIG);
    expect(p.mode).toBe("anchored");
    if (p.mode !== "anchored") return;
    expect(p.align).toBe("end");
  });

  it("keeps the panel on screen for a node in the top-right corner", () => {
    // The exact case in the bug report: clipped on the top AND the right.
    const canvas = { width: 1200, height: 800 };
    const p = placeInspector(node({ x: 900, y: 10 }), canvas);
    if (p.mode !== "anchored") {
      expect(p.width).toBeLessThanOrEqual(canvas.width - EDGE_MARGIN * 2);
      return;
    }
    expect(p.side).toBe("left");
    expect(p.align).toBe("start");
    const top = 10;
    expect(top + p.height).toBeLessThanOrEqual(canvas.height - EDGE_MARGIN);
  });

  it("does not produce negative sizes on a degenerate canvas", () => {
    const p = placeInspector(node(), { width: 10, height: 10 });
    expect(p.width).toBeGreaterThanOrEqual(0);
    expect(p.height).toBeGreaterThanOrEqual(0);
  });
});

describe("docked placement", () => {
  it("leaves room for the canvas toolbar it shares the top-right corner with", () => {
    const canvas = { width: 700, height: 600 };
    const p = placeInspector(node({ x: 216 }), canvas);
    expect(p.mode).toBe("docked");
    expect(p.height).toBeLessThanOrEqual(canvas.height - EDGE_MARGIN * 2 - DOCK_TOP_RESERVE);
  });
});
