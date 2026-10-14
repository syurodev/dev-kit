/**
 * Focusing an element that an inert ancestor holds.
 *
 * A shell panel stays inert while it opens and only becomes interactive once
 * it has settled (panelMotion.useClosedInert): lifting `inert` restyles the
 * whole panel, and that pass is kept out of the frames that move. A pane that
 * focuses itself when it opens (the references panel, for its keyboard
 * navigation) would have that focus() refused. This waits for the ancestor
 * to stop being inert and focuses then — unless the person has put focus
 * somewhere else in the meantime.
 */

type Node = unknown;

/** Whether a deferred focus still belongs where it was asked for. */
export function focusStillWanted(s: { requestedFrom: Node; now: Node; body: Node }): boolean {
  return s.now === null || s.now === s.body || s.now === s.requestedFrom;
}

/** Focus `el` now, or once no ancestor is inert. Returns a cancel. */
export function focusWhenInteractive(el: HTMLElement): () => void {
  if (!el.closest("[inert]")) {
    el.focus();
    return () => undefined;
  }
  const requestedFrom = document.activeElement;
  let observer: MutationObserver | null = null;
  const watch = () => {
    const blocker = el.closest("[inert]");
    if (!blocker) {
      observer = null;
      if (el.isConnected && focusStillWanted({ requestedFrom, now: document.activeElement, body: document.body })) el.focus();
      return;
    }
    observer = new MutationObserver(() => {
      if (blocker.hasAttribute("inert")) return;
      observer?.disconnect();
      watch();
    });
    observer.observe(blocker, { attributes: true, attributeFilter: ["inert"] });
  };
  watch();
  return () => observer?.disconnect();
}
