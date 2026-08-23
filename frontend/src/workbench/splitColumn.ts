import { GAP_WIDTH } from "./panelLayout";

/**
 * Floor for either half of the split right column.
 *
 * Enough for the smaller of the two to still be usable: a panel header plus a
 * tab row plus a few lines, or a chat composer plus one message. Lower than
 * CONTENT_MIN_HEIGHT because these are companions to the centre content, not
 * the content itself.
 */
export const SPLIT_MIN_HEIGHT = 180;

export type SplitSizes = { top: number; divider: number; bottom: number };

/**
 * Divides the right column between its two stacked slots.
 *
 * `topHeight` is the remembered drag position, which is stored as an absolute
 * number and therefore outlives the window it was chosen in. Every clamp
 * happens here, at render, rather than when the value is written — the same
 * rule the bottom slot follows, so a value from a taller window is never
 * persisted as a smaller one just because it was once displayed clamped.
 *
 * Always returns three parts summing exactly to columnHeight, so the caller
 * never has to reconcile a rounding gap against a flex container.
 */
export function splitColumn(opts: {
  columnHeight: number;
  topOpen: boolean;
  bottomOpen: boolean;
  topHeight: number;
}): SplitSizes {
  const { columnHeight, topOpen, bottomOpen, topHeight } = opts;

  if (!topOpen && !bottomOpen) return { top: 0, divider: 0, bottom: 0 };
  if (topOpen && !bottomOpen) return { top: columnHeight, divider: 0, bottom: 0 };
  if (!topOpen && bottomOpen) return { top: 0, divider: 0, bottom: columnHeight };

  const divider = GAP_WIDTH;
  const usable = columnHeight - divider;

  // Too small to honour both floors: split it down the middle rather than
  // handing one slot a negative height.
  if (usable < SPLIT_MIN_HEIGHT * 2) {
    const top = Math.floor(usable / 2);
    return { top, divider, bottom: usable - top };
  }

  const top = Math.min(Math.max(topHeight, SPLIT_MIN_HEIGHT), usable - SPLIT_MIN_HEIGHT);
  return { top, divider, bottom: usable - top };
}
