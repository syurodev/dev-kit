import { getSvgPath } from "figma-squircle";

/**
 * Continuous ("squircle") rounded rectangles, the corner macOS draws its
 * windows with. CSS border-radius draws a circular arc, and WebKit has no
 * `corner-shape` yet, so a card that should run parallel to the window corner
 * is clipped to this path instead (see useShellSquircle).
 *
 * The geometry is Figma's corner smoothing, from figma-squircle: a circular
 * arc in the middle of each corner, eased into the straight edges by two cubic
 * Béziers. Smoothing 0 is a plain circular corner.
 */

/** Apple's continuous corner, as Figma's "iOS" preset approximates it. */
export const APPLE_CORNER_SMOOTHING = 0.6;

/** The SVG path of a width × height rectangle with continuous corners. */
export function squirclePath(width: number, height: number, radius: number, smoothing = APPLE_CORNER_SMOOTHING): string {
  return getSvgPath({
    width: Math.max(0, width),
    height: Math.max(0, height),
    cornerRadius: Math.max(0, radius),
    cornerSmoothing: smoothing,
  });
}
