/**
 * Focusing an element that an inert ancestor holds.
 *
 * A shell panel stays inert while it opens and only becomes interactive once
 * it has settled (panelMotion.useClosedInert): lifting `inert` restyles the
 * whole panel, and that pass is kept out of the frames that move. A pane that
 * focuses itself when it opens (the references panel, for its keyboard
 * navigation) would have that focus() refused. This waits for the ancestor
 * to stop being inert and focuses then — unless the person has put focus
 * somewhere else in the meantime, the caller cancels (the pane closed again),
 * or the panel has not settled within GIVE_UP_MS (past any motion's watchdog).
 */

/** Longer than any panel motion and its watchdog (panelMotion). */
const GIVE_UP_MS = 1000;

/** Whether a deferred focus still belongs where it was asked for. */
export function focusStillWanted(s: { requestedFrom: unknown; now: unknown; body: unknown }): boolean {
  return s.now === null || s.now === s.body || s.now === s.requestedFrom;
}

/** What focusWhenInteractive touches, so it can be tested without a DOM. */
export type FocusEnv<E> = {
  inertAncestor: (el: E) => object | null;
  /** Calls `onChange` when the blocker's inert attribute changes; returns a disconnect. */
  watchInert: (blocker: object, onChange: () => void) => () => void;
  focus: (el: E) => void;
  active: () => unknown;
  body: unknown;
  connected: (el: E) => boolean;
  setTimer: (fn: () => void, ms: number) => unknown;
  clearTimer: (id: unknown) => void;
};

const domEnv = (): FocusEnv<HTMLElement> => ({
  inertAncestor: (el) => el.closest("[inert]"),
  watchInert: (blocker, onChange) => {
    const observer = new MutationObserver(onChange);
    observer.observe(blocker as Element, { attributes: true, attributeFilter: ["inert"] });
    return () => observer.disconnect();
  },
  focus: (el) => el.focus(),
  active: () => document.activeElement,
  body: document.body,
  connected: (el) => el.isConnected,
  setTimer: (fn, ms) => setTimeout(fn, ms),
  clearTimer: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
});

/** Focus `el` now, or once no ancestor is inert. Returns a cancel. */
export function focusWhenInteractive<E = HTMLElement>(el: E, env: FocusEnv<E> = domEnv() as unknown as FocusEnv<E>): () => void {
  if (!env.inertAncestor(el)) {
    env.focus(el);
    return () => undefined;
  }
  const requestedFrom = env.active();
  let disconnect: (() => void) | null = null;
  let timer: unknown = null;
  const stop = () => {
    disconnect?.();
    disconnect = null;
    if (timer !== null) env.clearTimer(timer);
    timer = null;
  };
  const watch = () => {
    const blocker = env.inertAncestor(el);
    if (!blocker) {
      stop();
      if (env.connected(el) && focusStillWanted({ requestedFrom, now: env.active(), body: env.body })) env.focus(el);
      return;
    }
    disconnect?.();
    disconnect = env.watchInert(blocker, () => {
      if (env.inertAncestor(el) !== blocker) watch();
    });
  };
  timer = env.setTimer(stop, GIVE_UP_MS);
  watch();
  return stop;
}
