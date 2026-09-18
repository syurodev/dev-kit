import { APPLE_CORNER_SMOOTHING, squirclePath } from "./squircle";

/** Set on <html> by main.tsx in the macOS window only. */
export const SQUIRCLE_SHELL_CLASS = "squircle-shell";

/** Whether this document draws the shell's cards with continuous corners. */
export function squircleShellEnabled(): boolean {
  return typeof document !== "undefined" && document.documentElement.classList.contains(SQUIRCLE_SHELL_CLASS);
}

/**
 * The ring drawn in place of the card's border: the same continuous shape,
 * half a pixel in so a 1px stroke lands on whole pixels.
 */
export function shellRingImage(width: number, height: number, radius: number, color: string): string {
  const d = squirclePath(width - 1, height - 1, Math.max(0, radius - 0.5), APPLE_CORNER_SMOOTHING);
  const svg = `<svg xmlns='http://www.w3.org/2000/svg' width='${width}' height='${height}'><path transform='translate(0.5 0.5)' d='${d}' fill='none' stroke='${color}' stroke-width='1'/></svg>`;
  return `url("data:image/svg+xml,${encodeURIComponent(svg)}")`;
}

/**
 * Gives a `.shell-surface` card the macOS window's continuous corner, so it
 * runs parallel to the window corner instead of cutting across it. A callback
 * ref — `ref={shellSquircleRef}` — rather than a hook, because two of the
 * three cards mount only while their panel is open, and a ref is told when
 * that happens. Module-level, so its identity never changes and React attaches
 * it once per element; the returned function is React 19's ref cleanup.
 *
 * macOS draws window corners as a squircle; CSS border-radius only draws a
 * circular arc, and WebKit does not support `corner-shape` yet. So in the
 * macOS window (SQUIRCLE_SHELL_CLASS) the card is clipped to the squircle
 * path, and — because the clip would cut its border away at the corners — its
 * border is redrawn as an SVG ring on ::after (index.css). The radius is still
 * the CSS one, --shell-radius: the path takes it from border-radius, so the
 * window-radius handshake and every other platform stay as they were.
 *
 * Re-drawn on resize (in the ResizeObserver callback, before paint, so a sash
 * drag never shows a stale corner), and on any change to <html>'s class, theme
 * or style: the ring's colour is resolved from --glass-border, and the window
 * radius arrives as --window-radius on <html>'s style.
 */
export function shellSquircleRef(el: HTMLElement | null): (() => void) | undefined {
  if (!el || !squircleShellEnabled()) return undefined;

  const apply = () => {
    // Layout sizes, not getBoundingClientRect: a card mid-animation is scaled
    // by a transform, and the path is in its own coordinates.
    const width = el.offsetWidth;
    const height = el.offsetHeight;
    if (!width || !height) return;
    const style = getComputedStyle(el);
    const radius = parseFloat(style.borderTopLeftRadius) || 0;
    el.style.clipPath = `path('${squirclePath(width, height, radius)}')`;
    el.style.setProperty("--shell-ring", shellRingImage(width, height, radius, style.columnRuleColor));
  };

  apply();
  const resize = new ResizeObserver(apply);
  resize.observe(el);
  // Theme and palette live on <html>; so does --window-radius, in style.
  const theme = new MutationObserver(apply);
  theme.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["class", "data-theme", "data-theme-family", "style"],
  });
  return () => {
    resize.disconnect();
    theme.disconnect();
    el.style.removeProperty("clip-path");
    el.style.removeProperty("--shell-ring");
  };
}
