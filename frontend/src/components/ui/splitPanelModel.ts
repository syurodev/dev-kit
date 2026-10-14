/**
 * The module context panes' state, as pure steps (split-panel.tsx holds it).
 *
 * A pane a module closes is hidden, not dropped: its body stays mounted where
 * it last was and reopening renders the same tree in place, as the Agent
 * companion does — mounting an inspector fresh was the whole first frame of
 * its open (~70 ms on WebKitGTK). It is dropped when the module asks
 * (`discard`, for a body that must not outlive its close), when the caller
 * that opened it unmounts, or when its module leaves the screen.
 *
 * `generation` is the body's identity: a new one is a new tree. The module
 * names what a body is with `key` (the flow, the note, "nav" for the
 * references panel); a different key is a new tree, the same key keeps the
 * tree whoever opens it — choosing a reference opens it from another
 * document, and the list must not rebuild. A discard starts a new one too.
 * Who opened it (`opener`) only decides whose unmount drops it and whose
 * close counts.
 */
export type PanelEntry<C> = {
  content: C;
  width: number;
  onClose: () => void;
  /** What the body is; a different key is a new tree. */
  key?: string;
  /** Mounted but not shown. */
  hidden?: boolean;
  /** Which caller (a document's useSplitPanelActions) last opened it. */
  opener?: string;
  generation: number;
};

export type PanelsState<C = unknown> = {
  panels: Record<string, PanelEntry<C>>;
  /** Next generation per owner, kept across a discard. */
  gens: Record<string, number>;
};

export const EMPTY_PANELS: PanelsState<never> = { panels: {}, gens: {} };

export function openStep<C>(
  prev: PanelsState<C>,
  owner: string,
  next: { content: C; width: number; onClose: () => void; key?: string },
  opener: string,
): PanelsState<C> {
  const was = prev.panels[owner];
  let gens = prev.gens;
  if (was && was.key !== next.key) gens = { ...gens, [owner]: (gens[owner] ?? 0) + 1 };
  const generation = gens[owner] ?? 0;
  return { panels: { ...prev.panels, [owner]: { ...next, hidden: false, opener, generation } }, gens };
}

/** `opener` closes its module's pane: hidden, or dropped on `discard`. Ignored unless it is the one showing it. */
export function closeStep<C>(prev: PanelsState<C>, owner: string, discard: boolean, opener: string): PanelsState<C> {
  const was = prev.panels[owner];
  if (!was || was.opener !== opener) return prev;
  if (discard) return drop(prev, owner);
  if (was.hidden) return prev;
  return { ...prev, panels: { ...prev.panels, [owner]: { ...was, hidden: true } } };
}

/** The caller `opener` unmounted: the pane goes with it if it was the one that opened it. */
export function releaseStep<C>(prev: PanelsState<C>, owner: string, opener: string): PanelsState<C> {
  return prev.panels[owner]?.opener === opener ? drop(prev, owner) : prev;
}

/**
 * Hidden panes of modules no longer on screen are dropped: their bodies are
 * not drawn anywhere (only modules on screen have bodies), and a reopen
 * mounts a fresh one either way — the entry would only hold the content
 * element, and whatever it closes over, alive.
 */
export function forgetHiddenStep<C>(prev: PanelsState<C>, onScreen: readonly string[]): PanelsState<C> {
  let next = prev;
  for (const [owner, p] of Object.entries(prev.panels)) if (p.hidden && !onScreen.includes(owner)) next = drop(next, owner);
  return next;
}

function drop<C>(prev: PanelsState<C>, owner: string): PanelsState<C> {
  const { [owner]: _, ...panels } = prev.panels;
  return { panels, gens: { ...prev.gens, [owner]: (prev.gens[owner] ?? 0) + 1 } };
}

/** The panes that are open; the same object when none is hidden. */
export function shownPanels<C>(panels: Record<string, PanelEntry<C>>): Record<string, PanelEntry<C>> {
  const entries = Object.entries(panels);
  if (!entries.some(([, p]) => p.hidden)) return panels;
  return Object.fromEntries(entries.filter(([, p]) => !p.hidden));
}

export type CompanionEntry<C> = { content: C; width: number; onClose: () => void; hidden?: boolean };

export type SplitPanelState<C> = PanelsState<C> & { companion: CompanionEntry<C> | null };

export type PanelOpenOptions = {
  width?: number;
  onClose: () => void;
  /** What the body shows (see PanelEntry.key). Omitted, every open is the same tree. */
  key?: string;
};
export type PanelCloseOptions = {
  /**
   * Drop the body instead of keeping it hidden: for a pane whose body must
   * not outlive its close (one that polls, follows a live stream, or must be
   * fresh each time it is opened).
   */
  discard?: boolean;
};

export type SplitPanelActions<C> = {
  openPanelFor: (owner: string, content: C, opts: PanelOpenOptions, opener: string) => void;
  closePanelFor: (owner: string, opener: string, opts?: PanelCloseOptions) => void;
  /** `opener` unmounted: drop the pane if it was the one that opened it. */
  releasePanelFor: (owner: string, opener: string) => void;
  /** Drop hidden panes of modules not in `onScreen`. */
  forgetHiddenPanels: (onScreen: readonly string[]) => void;
  openCompanion: (content: C, opts: { onClose: () => void; hidden?: boolean }) => void;
  closeCompanion: () => void;
};

/** What a caller (a document) uses: its own module's pane and the companion. */
export type BoundPanelActions<C> = {
  openPanel: (content: C, opts: PanelOpenOptions) => void;
  closePanel: (opts?: PanelCloseOptions) => void;
  openCompanion: SplitPanelActions<C>["openCompanion"];
  closeCompanion: SplitPanelActions<C>["closeCompanion"];
};

const DEFAULT_WIDTH = 420;

/**
 * The shell's split-panel state as a small external store. Its actions are
 * made once and never change, and each caller's bound actions are one object
 * for its lifetime (`bound`), so a document that only opens and closes its
 * pane holds no subscription and does not re-render when any pane toggles;
 * the few readers of what is open subscribe (useSplitPanelState).
 *
 * A caller is `attach`ed while mounted. `detach` drops the pane it opened, but
 * a moment later (`defer`, a microtask): when one document replaces another in
 * a commit, the leaving one's cleanup runs before the arriving one opens the
 * pane, and StrictMode unmounts and remounts a caller in one flush.
 */
export function createSplitPanelStore<C>(opts: { defer?: (fn: () => void) => void } = {}) {
  const defer = opts.defer ?? ((fn: () => void) => queueMicrotask(fn));
  let state: SplitPanelState<C> = { ...(EMPTY_PANELS as PanelsState<C>), companion: null };
  const listeners = new Set<() => void>();
  const set = (next: SplitPanelState<C>) => {
    if (next === state) return;
    state = next;
    for (const fn of [...listeners]) fn();
  };
  const panelsOf = (next: PanelsState<C>) => (next === state ? state : { ...next, companion: state.companion });
  const actions: SplitPanelActions<C> = {
    openPanelFor: (owner, content, o, opener) =>
      set(panelsOf(openStep(state, owner, { content, width: o.width ?? DEFAULT_WIDTH, onClose: o.onClose, key: o.key }, opener))),
    closePanelFor: (owner, opener, o) => set(panelsOf(closeStep(state, owner, o?.discard ?? false, opener))),
    releasePanelFor: (owner, opener) => set(panelsOf(releaseStep(state, owner, opener))),
    forgetHiddenPanels: (onScreen) => set(panelsOf(forgetHiddenStep(state, onScreen))),
    // No width of its own: it shares the column, and letting it request one
    // would mean the column jumped every time the companion opened.
    openCompanion: (content, o) =>
      set({ ...state, companion: { content, width: 0, onClose: o.onClose, hidden: o.hidden ?? false } }),
    closeCompanion: () => {
      if (state.companion !== null) set({ ...state, companion: null });
    },
  };

  const callers = new Map<string, BoundPanelActions<C>>();
  const mounted = new Set<string>();
  const id = (owner: string, opener: string) => `${owner}\u0000${opener}`;

  return {
    actions,
    getState: () => state,
    subscribe: (fn: () => void) => {
      listeners.add(fn);
      return () => void listeners.delete(fn);
    },
    /** `actions` bound to one caller: its module (`owner`) and its identity (`opener`). */
    bound(owner: string, opener: string): BoundPanelActions<C> {
      const k = id(owner, opener);
      let b = callers.get(k);
      if (!b) {
        b = {
          openPanel: (content, o) => actions.openPanelFor(owner, content, o, opener),
          closePanel: (o) => actions.closePanelFor(owner, opener, o),
          openCompanion: actions.openCompanion,
          closeCompanion: actions.closeCompanion,
        };
        callers.set(k, b);
      }
      return b;
    },
    attach(owner: string, opener: string) {
      mounted.add(id(owner, opener));
    },
    detach(owner: string, opener: string) {
      const k = id(owner, opener);
      mounted.delete(k);
      defer(() => {
        if (mounted.has(k)) return;
        callers.delete(k);
        actions.releasePanelFor(owner, opener);
      });
    },
  };
}

export type SplitPanelStore<C> = ReturnType<typeof createSplitPanelStore<C>>;
