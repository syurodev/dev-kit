import * as React from "react";

type PanelState = {
  content: React.ReactNode;
  width: number;
  onClose: () => void;
};

type SplitPanelContextValue = {
  panel: PanelState | null;
  openPanel: (
    content: React.ReactNode,
    opts: { width?: number; onClose: () => void },
  ) => void;
  closePanel: () => void;
  companion: PanelState | null;
  openCompanion: (content: React.ReactNode, opts: { onClose: () => void }) => void;
  closeCompanion: () => void;
};

const SplitPanelContext = React.createContext<SplitPanelContextValue | null>(null);

/**
 * What the two right-hand panes hold: `panel` belongs to the active module and
 * dies with it; `companion` belongs to the shell and survives module switches,
 * which is where a conversation has to live — a module's subtree unmounts the
 * moment you leave it (see modules/codegraph/codegraphStore.ts).
 *
 * Only the content. Where each is drawn is the slot layout's business
 * (components/ui/pane-slots.tsx): `panel` is the context pane, `companion` the
 * Agent pane.
 */
export function SplitPanelProvider({ children }: { children: React.ReactNode }) {
  const [panel, setPanel] = React.useState<PanelState | null>(null);
  const [companion, setCompanion] = React.useState<PanelState | null>(null);

  const openPanel = React.useCallback(
    (content: React.ReactNode, opts: { width?: number; onClose: () => void }) => {
      setPanel({ content, width: opts.width ?? 420, onClose: opts.onClose });
    },
    [],
  );
  const closePanel = React.useCallback(() => setPanel(null), []);

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
    () => ({ panel, openPanel, closePanel, companion, openCompanion, closeCompanion }),
    [panel, openPanel, closePanel, companion, openCompanion, closeCompanion],
  );

  return (
    <SplitPanelContext.Provider value={value}>{children}</SplitPanelContext.Provider>
  );
}

export function useSplitPanel() {
  const ctx = React.useContext(SplitPanelContext);
  if (!ctx) throw new Error("useSplitPanel must be used within a SplitPanelProvider");
  return ctx;
}
