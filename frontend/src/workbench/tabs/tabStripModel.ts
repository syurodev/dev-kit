/**
 * The tab strip's arithmetic, kept out of the component so it can be tested
 * without a DOM. The behaviour follows VS Code's multi-editor tabs control.
 */

/**
 * A sortable drag ended with `fromId` over `overId`: what the store's
 * `reorderTab(fromId, beforeId)` needs, or undefined when nothing moved.
 *
 * The drag library reports the slot the tab was dropped into; the store takes
 * the id it now sits in front of (null for the end), because an id stays
 * right whatever else changed while the pointer was down.
 */
export function beforeIdAfterMove(
  ids: readonly string[],
  fromId: string,
  overId: string,
): string | null | undefined {
  const from = ids.indexOf(fromId);
  const to = ids.indexOf(overId);
  if (from === -1 || to === -1 || from === to) return undefined;
  const next = ids.slice();
  next.splice(from, 1);
  next.splice(to, 0, fromId);
  return next[to + 1] ?? null;
}

/**
 * The smallest scroll that shows a tab whole.
 *
 * Already visible: stay. Overflowing on the right and narrower than the
 * viewport: bring its right edge in. Anything else — off to the left, or wider
 * than the viewport — lines up its left edge.
 */
export function revealScrollLeft(view: {
  scrollLeft: number;
  viewportWidth: number;
  tabLeft: number;
  tabWidth: number;
}): number {
  const { scrollLeft, viewportWidth, tabLeft, tabWidth } = view;
  const tabRight = tabLeft + tabWidth;
  if (tabLeft >= scrollLeft && tabRight <= scrollLeft + viewportWidth) return scrollLeft;
  if (tabLeft > scrollLeft && tabWidth <= viewportWidth) return tabRight - viewportWidth;
  return tabLeft;
}
