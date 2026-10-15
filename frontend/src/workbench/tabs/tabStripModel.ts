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

/**
 * revealScrollLeft for a strip whose overflowing sides fade: a tab under a
 * fade reads as cut off, so each faded side's inset is taken off the viewport
 * before the tab is fitted into what is left.
 */
export function revealScrollLeftWithin(
  view: { scrollLeft: number; viewportWidth: number; tabLeft: number; tabWidth: number },
  insets: { left: number; right: number },
): number {
  const inner = revealScrollLeft({
    ...view,
    scrollLeft: view.scrollLeft + insets.left,
    viewportWidth: Math.max(0, view.viewportWidth - insets.left - insets.right),
  });
  return Math.max(0, inner - insets.left);
}

export type ScrollView = { scrollLeft: number; scrollWidth: number; clientWidth: number };

/** A pixel of slack: a fractional scrollLeft at either end is still the end. */
const EDGE_TOLERANCE = 1;

/** Which sides of the strip have tabs scrolled out of sight. */
export function overflowEdges(view: ScrollView): { left: boolean; right: boolean } {
  const max = view.scrollWidth - view.clientWidth;
  if (max <= EDGE_TOLERANCE) return { left: false, right: false };
  return { left: view.scrollLeft > EDGE_TOLERANCE, right: view.scrollLeft < max - EDGE_TOLERANCE };
}

/** How far a ‹ › press scrolls: most of a viewport, so a tab cut at the edge stays in sight. */
const STEP_SHARE = 0.8;

export function scrollStep(view: ScrollView, direction: "left" | "right"): number {
  const max = Math.max(0, view.scrollWidth - view.clientWidth);
  const delta = view.clientWidth * STEP_SHARE * (direction === "left" ? -1 : 1);
  return Math.min(max, Math.max(0, view.scrollLeft + delta));
}

export type TabMarkInput = { status?: "idle" | "running" | "success" | "error"; active: boolean; seenSuccess: boolean; dirty: boolean };
export type TabMarks = { status: "running" | "success" | "error" | null; dirty: boolean; hint: string[] };

/**
 * What a tab shows besides its title. Status sits before the title on every
 * tab; "finished" only until the tab is looked at (the active tab never
 * shows it — you are looking at it); unsaved work is a dot in the close slot.
 */
export function tabMarks(input: TabMarkInput): TabMarks {
  const hint: string[] = [];
  let status: TabMarks["status"] = null;
  if (input.status === "running") status = "running";
  else if (input.status === "error") status = "error";
  else if (input.status === "success" && !input.active && !input.seenSuccess) status = "success";
  if (status === "running") hint.push("Running");
  if (status === "error") hint.push("Failed");
  if (status === "success") hint.push("Finished");
  if (input.dirty) hint.push("Unsaved changes");
  return { status, dirty: input.dirty, hint };
}

/**
 * Finished tabs that have been looked at, after a change to the tabs: one on
 * screen (`shown`) while finished is seen; one whose status left success, or
 * that closed, is forgotten — the next finish is news again. The same set
 * when nothing changed, so a store holding it publishes nothing.
 */
export function nextSeen(
  seen: ReadonlySet<string>,
  tabs: readonly { id: string; status?: TabMarkInput["status"] }[],
  shown: Iterable<string>,
): ReadonlySet<string> {
  const finished = new Set(tabs.filter((tab) => tab.status === "success").map((tab) => tab.id));
  const next = new Set([...seen].filter((id) => finished.has(id)));
  for (const id of shown) if (finished.has(id)) next.add(id);
  return next.size === seen.size && [...next].every((id) => seen.has(id)) ? seen : next;
}
