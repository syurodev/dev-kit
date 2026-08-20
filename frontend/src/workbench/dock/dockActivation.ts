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
