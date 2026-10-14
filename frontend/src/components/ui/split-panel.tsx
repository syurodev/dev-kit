import * as React from "react";
import { SidebarModuleIdContext } from "@/components/ui/sidebar-shell";
import {
  createSplitPanelStore,
  type BoundPanelActions,
  type CompanionEntry,
  type PanelEntry,
  type SplitPanelState,
  type SplitPanelStore,
} from "@/components/ui/splitPanelModel";

export type { PanelCloseOptions, PanelOpenOptions } from "@/components/ui/splitPanelModel";

type PanelState = PanelEntry<React.ReactNode>;

/** A caller outside any module's subtree (the dev harness, the shell itself). */
export const SHELL_PANEL_OWNER = "shell";

const SplitPanelContext = React.createContext<SplitPanelStore<React.ReactNode> | null>(null);

/**
 * What the right-hand panes hold. Each module has its own `panel` — its context
 * pane — which it opens and closes from inside its views; `companion` belongs
 * to the shell and survives module switches, which is where a conversation has
 * to live.
 *
 * A panel belongs to the module that opened it, not to whichever module is
 * active: with two document groups on screen, Notes' Metadata and Code
 * Graph's inspector are both shown, each beside its own document
 * (docs/dev-kit/layout.mdx § Document group). useSplitPanelActions binds
 * openPanel to the caller's module through SidebarModuleIdContext, which every
 * module view already sits under, so no module had to change.
 *
 * The state lives in a small external store (splitPanelModel.ts) whose
 * actions never change: a document that only opens and closes its pane
 * (useSplitPanelActions) holds no subscription and does not re-render when a
 * pane toggles. The few readers of what is open use useSplitPanelState.
 *
 * Only the content. Where each is drawn is the slot layout's business
 * (components/ui/pane-slots.tsx).
 */
export function SplitPanelProvider({ children }: { children: React.ReactNode }) {
  const [store] = React.useState(() => createSplitPanelStore<React.ReactNode>());
  return <SplitPanelContext.Provider value={store}>{children}</SplitPanelContext.Provider>;
}

function useStore(): SplitPanelStore<React.ReactNode> {
  const store = React.useContext(SplitPanelContext);
  if (!store) throw new Error("split panel hooks must be used within a SplitPanelProvider");
  return store;
}

/**
 * Open and close the caller's module pane, and the companion. Stable for the
 * caller's lifetime and subscribed to nothing.
 *
 * A pane closed with `closePanel()` is hidden, not unmounted: its body stays
 * where it was and the next `openPanel` renders the same tree in place.
 * `closePanel({ discard: true })` drops it. When the caller unmounts (its
 * document closed, its module left the mounted set), a pane it opened is
 * dropped with it.
 */
export function useSplitPanelActions(): BoundPanelActions<React.ReactNode> {
  const store = useStore();
  const owner = React.useContext(SidebarModuleIdContext) ?? SHELL_PANEL_OWNER;
  const opener = React.useId();
  React.useEffect(() => {
    store.attach(owner, opener);
    return () => store.detach(owner, opener);
  }, [store, owner, opener]);
  return store.bound(owner, opener);
}

/** The store's own actions, for the shell (pane-slots.tsx). Stable. */
export function useSplitPanelStoreActions() {
  return useStore().actions;
}

/**
 * Reads what is open at the moment it is called, without subscribing: for an
 * action that needs it (the Layouts menu closing the module's pane).
 */
export function useSplitPanelSnapshot(): () => SplitPanelState<React.ReactNode> {
  return useStore().getState;
}


/**
 * What is open, for the few that draw or act on it (the slot layout, the
 * Layouts menu). Re-renders on every pane change; documents use
 * useSplitPanelActions instead.
 */
export function useSplitPanelState(): {
  /** Each module's context pane, keyed by module id; `hidden` when closed but kept. */
  panels: Record<string, PanelState>;
  companion: CompanionEntry<React.ReactNode> | null;
} {
  const store = useStore();
  const state: SplitPanelState<React.ReactNode> = React.useSyncExternalStore(store.subscribe, store.getState, store.getState);
  return { panels: state.panels, companion: state.companion };
}
