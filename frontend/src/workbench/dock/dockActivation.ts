/**
 * How far the pill rises when it opens, so the expanded dock is not glued to
 * the window edge the way the resting one is.
 *
 * Lifting moves the dock AWAY from the pointer that just opened it, which is
 * how a dock ends up flickering under a motionless mouse: open, fall outside
 * the moved zone, close, repeat. This used to be bounded by an activation
 * margin to prevent that. There is no margin any more, and isPointerOverDock
 * has no lower bound, so the zone can only grow when the pill rises and no
 * lift can strand a pointer below it. Free to change on how it looks.
 */
export const EXPAND_LIFT = 16;

export type Point = { x: number; y: number };
export type Bounds = { left: number; top: number; right: number; bottom: number };

/**
 * Whether the pointer is on the dock, and so whether the dock should be open.
 *
 * There is no proximity slack. The pill used to expand from up to 32px away on
 * every side, and that transparent approach zone is gone: an expanded dock is
 * 262x46 of opaque, event-claiming surface sitting over the bottom slot, and
 * anything that popped it open without the pointer being on it was putting
 * that surface in the way of whatever the slot is for.
 *
 * Kept as a rect test against the pill's own bounds rather than a CSS :hover
 * (or a padded hit area) for the one thing CSS cannot express — the open
 * bottom below. The pill is pinned a few px above the window edge, and those
 * px have to count or flicking the mouse to the bottom of the screen, the one
 * gesture the edge makes easy, would miss the target. Extending the pill's own
 * box down there instead would claim events in that strip; this claims none.
 *
 * Unbounded rather than a small bottom margin, and that is not tidiness: the
 * pill sits EXPAND_LIFT higher once open, so any fixed value big enough to
 * reach the window edge at rest falls EXPAND_LIFT short of it when expanded. A
 * pointer resting in those last rows would open the dock, find itself outside
 * the moved zone, close it, and flicker without moving.
 */
export function isPointerOverDock(pointer: Point, rect: Bounds): boolean {
  return pointer.x >= rect.left && pointer.x <= rect.right && pointer.y >= rect.top;
}

/** Modules past the ninth get no shortcut — there is no ⌘10. */
export function shortcutLabel(index: number): string | null {
  return index >= 0 && index < 9 ? `⌘${index + 1}` : null;
}

type KeyLike = {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
};

/**
 * Maps ⌘1–⌘9 (or Ctrl on non-mac) to a zero-based module index. Accepts
 * either modifier rather than branching on navigator.platform, which is
 * unreliable inside Wails' WKWebView — the same reasoning Workbench.tsx's
 * existing ⌘⇧E handler documents.
 */
export function moduleIndexFromEvent(event: KeyLike): number | null {
  if (!(event.metaKey || event.ctrlKey)) return null;
  if (event.altKey || event.shiftKey) return null;
  if (!/^[1-9]$/.test(event.key)) return null;
  return Number(event.key) - 1;
}
