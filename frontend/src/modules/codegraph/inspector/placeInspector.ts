/**
 * Where to put the node inspector so it stays readable in a small window.
 *
 * NodeToolbar anchors to a node with no idea the viewport exists, so a panel
 * beside a node near an edge runs off it. This decides from geometry alone
 * whether there is room to anchor, and gives up rather than render half
 * outside the canvas.
 *
 * Pure on purpose: every rule below is a pixel comparison, and pixel
 * comparisons are worth testing without mounting React Flow.
 */

/** Preferred size, matching the Figma popover. Shrinks, never grows. */
export const PREFERRED_WIDTH = 420;
export const PREFERRED_HEIGHT = 420;

/** Below this the panel stops being worth anchoring — it docks instead. */
export const MIN_ANCHORED_WIDTH = 300;

/**
 * Vertical space the docked panel leaves at the top of the canvas for
 * CanvasToolbar, which also lives at top-right. Anchored panels do not need it
 * — their width is already bounded by the room beside the node.
 */
export const DOCK_TOP_RESERVE = 48;

/** Gap between panel and node, and between panel and canvas edge. */
export const NODE_OFFSET = 16;
export const EDGE_MARGIN = 12;

export type Rect = { x: number; y: number; width: number; height: number };
export type Size = { width: number; height: number };

export type InspectorPlacement =
  | {
      mode: "anchored";
      /** NodeToolbar `position` — the side of the node the panel sits on. */
      side: "left" | "right";
      /** NodeToolbar `align` — how the panel lines up along the node's edge. */
      align: "start" | "center" | "end";
      width: number;
      height: number;
    }
  | { mode: "docked"; width: number; height: number };

/**
 * @param node    the node's rect in SCREEN pixels, relative to the canvas
 * @param canvas  the React Flow container's size
 */
export function placeInspector(node: Rect, canvas: Size): InspectorPlacement {
  const height = Math.min(PREFERRED_HEIGHT, Math.max(0, canvas.height - EDGE_MARGIN * 2));

  const roomRight = canvas.width - (node.x + node.width) - NODE_OFFSET - EDGE_MARGIN;
  const roomLeft = node.x - NODE_OFFSET - EDGE_MARGIN;
  const bestRoom = Math.max(roomRight, roomLeft);

  // Neither side can hold a panel worth reading. Docking keeps the whole panel
  // on screen at the cost of the visual tie to the node — the node stays
  // selected, and the Detail tab names it, so the tie is not lost entirely.
  if (bestRoom < MIN_ANCHORED_WIDTH) {
    return {
      mode: "docked",
      width: Math.min(PREFERRED_WIDTH, Math.max(0, canvas.width - EDGE_MARGIN * 2)),
      height: Math.min(
        PREFERRED_HEIGHT,
        Math.max(0, canvas.height - EDGE_MARGIN * 2 - DOCK_TOP_RESERVE),
      ),
    };
  }

  return {
    mode: "anchored",
    side: roomRight >= roomLeft ? "right" : "left",
    align: verticalAlign(node, canvas, height),
    width: Math.min(PREFERRED_WIDTH, bestRoom),
    height,
  };
}

/**
 * Picks the align that keeps the panel inside the canvas.
 *
 * NodeToolbar offers exactly three vertical anchors, so this is a choice among
 * three candidate tops rather than a free clamp. Ties go to `center` because
 * that is what reads as "belongs to this node"; the others are damage control.
 */
function verticalAlign(node: Rect, canvas: Size, height: number): "start" | "center" | "end" {
  const candidates = [
    { align: "center" as const, top: node.y + node.height / 2 - height / 2 },
    { align: "start" as const, top: node.y },
    { align: "end" as const, top: node.y + node.height - height },
  ];

  let best = candidates[0];
  let bestOverflow = Infinity;
  for (const c of candidates) {
    const overflow =
      Math.max(0, EDGE_MARGIN - c.top) + Math.max(0, c.top + height - (canvas.height - EDGE_MARGIN));
    if (overflow < bestOverflow) {
      best = c;
      bestOverflow = overflow;
    }
  }
  return best.align;
}
