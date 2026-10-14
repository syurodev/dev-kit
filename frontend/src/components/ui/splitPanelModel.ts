/**
 * The module context panes' state, as pure steps (split-panel.tsx holds it).
 *
 * A pane a module closes is hidden, not dropped: its body stays mounted where
 * it last was and reopening renders the same tree in place, as the Agent
 * companion does — mounting an inspector fresh was the whole first frame of
 * its open (~70 ms on WebKitGTK). It is dropped when the module asks
 * (`discard`, for a body that must not outlive its close) or when the
 * document that opened it unmounts.
 *
 * `generation` is the body's identity: a new one is a new tree. It changes
 * when a pane is reopened after a discard, and when a hidden pane is reopened
 * by another document than the one that left it — a body showing one note's
 * history must not be what another note's history first draws.
 */
export type PanelEntry<C> = {
  content: C;
  width: number;
  onClose: () => void;
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
  next: { content: C; width: number; onClose: () => void },
  opener: string,
): PanelsState<C> {
  const was = prev.panels[owner];
  let gens = prev.gens;
  if (was?.hidden && was.opener !== opener) gens = { ...gens, [owner]: (gens[owner] ?? 0) + 1 };
  const generation = gens[owner] ?? 0;
  return { panels: { ...prev.panels, [owner]: { ...next, hidden: false, opener, generation } }, gens };
}

export function closeStep<C>(prev: PanelsState<C>, owner: string, discard: boolean): PanelsState<C> {
  const was = prev.panels[owner];
  if (!was) return prev;
  if (discard) return drop(prev, owner);
  if (was.hidden) return prev;
  return { ...prev, panels: { ...prev.panels, [owner]: { ...was, hidden: true } } };
}

/** The caller `opener` unmounted: the pane goes with it if it was the one that opened it. */
export function releaseStep<C>(prev: PanelsState<C>, owner: string, opener: string): PanelsState<C> {
  return prev.panels[owner]?.opener === opener ? drop(prev, owner) : prev;
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

export type PanelOpenOptions = { width?: number; onClose: () => void };
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
  closePanelFor: (owner: string, opts?: PanelCloseOptions) => void;
  /** `opener` unmounted: drop the pane if it was the one that opened it. */
  releasePanelFor: (owner: string, opener: string) => void;
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
 * made once and never change, so a document that only opens and closes its
 * pane holds no subscription and does not re-render when any pane toggles;
 * the few readers of what is open subscribe (useSplitPanelState).
 */
export function createSplitPanelStore<C>() {
  let state: SplitPanelState<C> = { ...(EMPTY_PANELS as PanelsState<C>), companion: null };
  const listeners = new Set<() => void>();
  const set = (next: SplitPanelState<C>) => {
    if (next === state) return;
    state = next;
    for (const fn of [...listeners]) fn();
  };
  const panelsOf = (next: PanelsState<C>) => (next === state ? state : { ...next, companion: state.companion });
  const actions: SplitPanelActions<C> = {
    openPanelFor: (owner, content, opts, opener) =>
      set(panelsOf(openStep(state, owner, { content, width: opts.width ?? DEFAULT_WIDTH, onClose: opts.onClose }, opener))),
    closePanelFor: (owner, opts) => set(panelsOf(closeStep(state, owner, opts?.discard ?? false))),
    releasePanelFor: (owner, opener) => set(panelsOf(releaseStep(state, owner, opener))),
    // No width of its own: it shares the column, and letting it request one
    // would mean the column jumped every time the companion opened.
    openCompanion: (content, opts) =>
      set({ ...state, companion: { content, width: 0, onClose: opts.onClose, hidden: opts.hidden ?? false } }),
    closeCompanion: () => {
      if (state.companion !== null) set({ ...state, companion: null });
    },
  };
  return {
    actions,
    getState: () => state,
    subscribe: (fn: () => void) => {
      listeners.add(fn);
      return () => void listeners.delete(fn);
    },
  };
}

export type SplitPanelStore<C> = ReturnType<typeof createSplitPanelStore<C>>;

/** `actions` bound to one caller: its module (`owner`) and its own identity (`opener`). */
export function bindPanelActions<C>(actions: SplitPanelActions<C>, owner: string, opener: string): BoundPanelActions<C> {
  return {
    openPanel: (content, opts) => actions.openPanelFor(owner, content, opts, opener),
    closePanel: (opts) => actions.closePanelFor(owner, opts),
    openCompanion: actions.openCompanion,
    closeCompanion: actions.closeCompanion,
  };
}
