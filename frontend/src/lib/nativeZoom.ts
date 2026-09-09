/**
 * Stops the webview from zooming the whole application.
 *
 * A trackpad pinch reaches the page as a `wheel` event with `ctrlKey` set —
 * Chrome started that convention around 2015 and every engine followed, so it
 * is how a web app tells a pinch from a two-finger scroll. Left unprevented,
 * the host webview reads the same event as "zoom the page" and scales the
 * entire window: title bar, sidebar, dock and all.
 *
 * That is never what someone pinching inside a canvas meant. DevKit is an
 * application, not a document — nothing in the shell is a thing to zoom, and
 * the one surface where a pinch IS meaningful (the CodeGraph canvas) does its
 * own zooming and wants the gesture for itself.
 *
 * React Flow already prevents the default over its own pane, so this is not a
 * replacement for that. It closes the gap around it: a pinch that lands on a
 * node card, the canvas header, the legend, or anywhere else in the app is
 * still a pinch, and without this it zooms the window instead of doing nothing.
 *
 * Prevention does not stop propagation, so the canvas still receives the event
 * and still zooms. Only the host's own interpretation is suppressed.
 */

/**
 * Whether this wheel event is a zoom gesture rather than a scroll.
 *
 * Split out so the rule is testable without a DOM: it is one condition, and it
 * is the whole behaviour.
 */
export function isZoomGesture(event: Pick<WheelEvent, "ctrlKey">): boolean {
  return event.ctrlKey;
}

/**
 * Installs the guard on a window, returning the function that removes it.
 *
 * Capture phase so it runs before anything downstream can stop propagation,
 * and `passive: false` because a passive listener is forbidden from calling
 * preventDefault — a passive registration here would silently do nothing,
 * which is the failure mode worth naming.
 */
export function installNativeZoomGuard(target: Window): () => void {
  const onWheel = (event: WheelEvent) => {
    if (isZoomGesture(event)) {
      event.preventDefault();
    }
  };
  target.addEventListener("wheel", onWheel, { capture: true, passive: false });
  return () => target.removeEventListener("wheel", onWheel, { capture: true });
}
