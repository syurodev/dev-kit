/**
 * How far from the pill the pointer expands the dock.
 *
 * Started at 64 and came down: the pill rests 8px off the window bottom and is
 * 15px tall, so 64 opened the dock while the pointer was still 80px up the
 * window — a long way from anything that looks clickable.
 */
export const ACTIVATION_MARGIN = 32;

/**
 * How far the pill rises when it opens, so the expanded dock is not glued to
 * the window edge the way the resting one is.
 *
 * Bounded by ACTIVATION_MARGIN, and the reason is subtle enough that
 * dockActivation.test.ts asserts it: lifting moves the dock away from the
 * pointer that just opened it, so a pointer resting at the edge of the trigger
 * zone could open it, fall out of the zone, close it, and oscillate forever.
 * That does not happen here only because the pill grows UPWARD by 31px as it
 * opens — more than it lifts — and because the margin below it already reaches
 * past the window edge, where no pointer can be.
 */
export const EXPAND_LIFT = 16;

export type Point = { x: number; y: number };
export type Bounds = { left: number; top: number; right: number; bottom: number };

/**
 * Whether the pointer is close enough to expand the dock.
 *
 * Deliberately a distance test against the pill's real rect rather than an
 * invisible padded hit area: a padded `pointer-events-auto` element would
 * intercept clicks and drags across the bottom band and break text selection
 * in whatever is rendered there. This keeps the activation distance generous
 * while the dock still claims events only where it actually draws.
 */
export function isPointerNear(pointer: Point, rect: Bounds, margin: number): boolean {
  return (
    pointer.x >= rect.left - margin &&
    pointer.x <= rect.right + margin &&
    pointer.y >= rect.top - margin &&
    pointer.y <= rect.bottom + margin
  );
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
