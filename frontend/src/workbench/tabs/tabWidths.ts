/**
 * How wide each tab in the strip is.
 *
 * Pulled out of TabStrip because the arithmetic is the whole behaviour and it
 * is worth testing without a DOM: the strip measured 889px for three tabs,
 * capped them at 200px each, and left 221px of the row empty beside a label it
 * had truncated.
 */

/**
 * The most a tab is given when there is room to spare.
 *
 * A cap at all because two short-titled tabs should not stretch into giant
 * mostly-empty pills. 320 rather than 200 because the old cap bit long before
 * the row was full — a schema of `backup_his_medical_records_*` tables
 * truncates every one of them at 200px.
 */
export const MAX_TAB_WIDTH_PX = 320;

/**
 * The width below which the active tab stops shrinking with the others.
 *
 * Must stay at or under MAX_TAB_WIDTH_PX: above it, a roomy strip would render
 * the active tab NARROWER than a cramped one, which is backwards for a floor.
 * It is no longer equal to the cap — the cap rose and this did not, because
 * this is about keeping the active tab readable when the row is full, not
 * about how wide a tab may get when it is empty.
 */
export const ACTIVE_MIN_WIDTH_PX = 200;

/**
 * Floor for an inactive tab: pl-3 + icon + pr-8 (reserved for close) is 60px at
 * rest; 52px still shows the icon whole.
 */
export const INACTIVE_MIN_WIDTH_PX = 52;

/** Gap between two tabs, matching the row's own `gap` (4px). */
const TAB_GAP_PX = 4;

/**
 * What the strip's measured box spends on something other than tabs: its own
 * `p-2` either side, the + button, and the `gap-2` on BOTH sides of it — the
 * row is [tabs] gap [+] gap [spacer], so there are two.
 *
 * Dividing the whole measured width among tabs — which is what the strip did —
 * over-counts by this much. It went unnoticed only because the 200px cap
 * swallowed the error on every row that had room.
 */
const CHROME_PX = 16 + 8 + 28 + 8;

export type TabWidths = {
  /** Width per tab id; undefined means "auto", before the first measurement. */
  widths: Map<string, number | undefined>;
};

/**
 * Every tab gets the same width, capped, until the active one would drop below
 * its floor — from there the active tab holds and the rest keep splitting what
 * is left, down to their own floor, past which the strip scrolls instead.
 */
export function computeTabWidths(opts: {
  tabIds: readonly string[];
  activeTabId: string;
  /** The strip's own clientWidth, or undefined before it has been measured. */
  containerWidth: number | undefined;
}): Map<string, number | undefined> {
  const { tabIds, activeTabId, containerWidth } = opts;
  const widths = new Map<string, number | undefined>();

  if (containerWidth === undefined || tabIds.length === 0) {
    tabIds.forEach((id) => widths.set(id, undefined));
    return widths;
  }

  const forTabs = containerWidth - CHROME_PX - (tabIds.length - 1) * TAB_GAP_PX;
  const equalShare = forTabs / tabIds.length;
  const hasVisibleActive = tabIds.includes(activeTabId);

  if (!hasVisibleActive || equalShare >= ACTIVE_MIN_WIDTH_PX) {
    const share = Math.min(
      MAX_TAB_WIDTH_PX,
      Math.max(equalShare, INACTIVE_MIN_WIDTH_PX),
    );
    tabIds.forEach((id) => widths.set(id, share));
    return widths;
  }

  const remaining = forTabs - ACTIVE_MIN_WIDTH_PX;
  const inactiveCount = tabIds.length - 1;
  const inactiveShare =
    inactiveCount > 0
      ? Math.max(INACTIVE_MIN_WIDTH_PX, remaining / inactiveCount)
      : undefined;
  tabIds.forEach((id) => {
    widths.set(id, id === activeTabId ? ACTIVE_MIN_WIDTH_PX : inactiveShare);
  });
  return widths;
}
