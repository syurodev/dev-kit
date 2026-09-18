import * as React from "react";
import { SidebarModuleIdContext } from "@/components/ui/sidebar-shell";

type PanelState = {
  content: React.ReactNode;
  width: number;
  onClose: () => void;
};

/** A caller outside any module's subtree (the dev harness, the shell itself). */
export const SHELL_PANEL_OWNER = "shell";

type SplitPanelStore = {
  /** Each module's context pane, keyed by module id. */
  panels: Record<string, PanelState>;
  openPanelFor: (owner: string, content: React.ReactNode, opts: { width?: number; onClose: () => void }) => void;
  closePanelFor: (owner: string) => void;
  companion: PanelState | null;
  openCompanion: (content: React.ReactNode, opts: { onClose: () => void }) => void;
  closeCompanion: () => void;
};

type SplitPanelContextValue = SplitPanelStore & {
  /** The calling module's own panel. */
  panel: PanelState | null;
  openPanel: (content: React.ReactNode, opts: { width?: number; onClose: () => void }) => void;
  closePanel: () => void;
};

const SplitPanelContext = React.createContext<SplitPanelStore | null>(null);

/**
 * What the right-hand panes hold. Each module has its own `panel` — its context
 * pane — which it opens and closes from inside its views; `companion` belongs
 * to the shell and survives module switches, which is where a conversation has
 * to live.
 *
 * A panel belongs to the module that opened it, not to whichever module is
 * active: with two document groups on screen, Notes' Metadata and Code
 * Graph's inspector are both shown, each beside its own document
 * (docs/dev-kit/layout.mdx § Document group). useSplitPanel binds openPanel
 * to the caller's module through SidebarModuleIdContext, which every module
 * view already sits under, so no module had to change.
 *
 * Only the content. Where each is drawn is the slot layout's business
 * (components/ui/pane-slots.tsx).
 */
export function SplitPanelProvider({ children }: { children: React.ReactNode }) {
  const [panels, setPanels] = React.useState<Record<string, PanelState>>({});
  const [companion, setCompanion] = React.useState<PanelState | null>(null);

  const openPanelFor = React.useCallback(
    (owner: string, content: React.ReactNode, opts: { width?: number; onClose: () => void }) => {
      setPanels((prev) => ({ ...prev, [owner]: { content, width: opts.width ?? 420, onClose: opts.onClose } }));
    },
    [],
  );
  const closePanelFor = React.useCallback((owner: string) => {
    setPanels((prev) => {
      if (!(owner in prev)) return prev;
      const { [owner]: _, ...rest } = prev;
      return rest;
    });
  }, []);

  const openCompanion = React.useCallback(
    (content: React.ReactNode, opts: { onClose: () => void }) => {
      // No width of its own: it shares the column, and letting it request one
      // would mean the column jumped every time the companion opened.
      setCompanion({ content, width: 0, onClose: opts.onClose });
    },
    [],
  );
  const closeCompanion = React.useCallback(() => setCompanion(null), []);

  const value = React.useMemo(
    () => ({ panels, openPanelFor, closePanelFor, companion, openCompanion, closeCompanion }),
    [panels, openPanelFor, closePanelFor, companion, openCompanion, closeCompanion],
  );

  return <SplitPanelContext.Provider value={value}>{children}</SplitPanelContext.Provider>;
}

export function useSplitPanel(): SplitPanelContextValue {
  const ctx = React.useContext(SplitPanelContext);
  if (!ctx) throw new Error("useSplitPanel must be used within a SplitPanelProvider");
  const owner = React.useContext(SidebarModuleIdContext) ?? SHELL_PANEL_OWNER;
  const { openPanelFor, closePanelFor } = ctx;
  // Stable per owner: modules list these in effect dependencies.
  const openPanel = React.useCallback(
    (content: React.ReactNode, opts: { width?: number; onClose: () => void }) => openPanelFor(owner, content, opts),
    [openPanelFor, owner],
  );
  const closePanel = React.useCallback(() => closePanelFor(owner), [closePanelFor, owner]);
  return { ...ctx, panel: ctx.panels[owner] ?? null, openPanel, closePanel };
}
