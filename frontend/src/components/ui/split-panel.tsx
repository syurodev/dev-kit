import * as React from "react";
import { AnimatePresence, LazyMotion, domAnimation, m } from "framer-motion";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

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
};

const SplitPanelContext = React.createContext<SplitPanelContextValue | null>(null);

export function SplitPanelProvider({ children }: { children: React.ReactNode }) {
  const [panel, setPanel] = React.useState<PanelState | null>(null);

  const openPanel = React.useCallback(
    (content: React.ReactNode, opts: { width?: number; onClose: () => void }) => {
      setPanel({ content, width: opts.width ?? 420, onClose: opts.onClose });
    },
    [],
  );
  const closePanel = React.useCallback(() => setPanel(null), []);

  const value = React.useMemo(
    () => ({ panel, openPanel, closePanel }),
    [panel, openPanel, closePanel],
  );

  return (
    <SplitPanelContext.Provider value={value}>{children}</SplitPanelContext.Provider>
  );
}

export function useSplitPanel(): SplitPanelContextValue {
  const ctx = React.useContext(SplitPanelContext);
  if (!ctx) {
    throw new Error("useSplitPanel must be used within a SplitPanelProvider.");
  }
  return ctx;
}

/**
 * Renders as a flex sibling of the active module's content (see
 * Workbench.tsx). Its own LazyMotion wrap is required — it is not a
 * descendant of Dock's LazyMotion (siblings under Workbench, not
 * parent/child).
 */
export function SplitPanelSlot() {
  const { panel } = useSplitPanel();

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ width: 0 }}
        animate={{ width: panel ? panel.width : 0 }}
        transition={{ duration: 0.22, ease: "easeInOut" }}
        className={cn(
          "relative flex min-h-0 shrink-0 flex-col overflow-hidden",
          panel ? "border-l border-border" : "",
        )}
      >
        {panel ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="Close panel"
            className="absolute top-2 right-2 z-10"
            onClick={panel.onClose}
          >
            <X className="size-4" />
          </Button>
        ) : null}
        <AnimatePresence>
          {panel ? (
            <m.div
              key="panel-content"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.15 }}
              className="flex min-h-0 flex-1 flex-col overflow-auto"
            >
              {panel.content}
            </m.div>
          ) : null}
        </AnimatePresence>
      </m.div>
    </LazyMotion>
  );
}
