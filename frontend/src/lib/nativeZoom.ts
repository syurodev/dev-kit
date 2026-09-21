/**
 * Stops the webview from zooming the whole application.
 *
 * A trackpad pinch arrives as a `wheel` event with `ctrlKey` set — the
 * convention every engine follows. Unprevented, the host reads it as "zoom the
 * page" and scales the window: title bar, sidebar, dock and all. DevKit is an
 * application, not a document, so nothing in the shell is a thing to zoom.
 *
 * React Flow already prevents the default over its own pane; this closes the
 * gap around it, where a pinch on a node card, the header or the legend would
 * otherwise zoom the window. Preventing does not stop propagation, so the
 * canvas still gets the event and still zooms — only the host's own
 * interpretation is suppressed.
 *
 * This covers macOS and Windows, where the OS turns a trackpad pinch into
 * exactly that synthetic `wheel`+`ctrlKey` event. WebKitGTK on Linux instead
 * drives its own page zoom natively, without ever firing a `wheel` event —
 * nothing here sees it, let alone prevents it. See lockWebviewZoom in
 * native_webview_linux.go for that platform's guard.
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
