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

const MIN_PANEL_WIDTH = 320;
const MAX_PANEL_WIDTH = 720;

/**
 * Renders as a flex sibling of the active module's content (see
 * Workbench.tsx). Its own LazyMotion wrap is required — it is not a
 * descendant of Dock's LazyMotion (siblings under Workbench, not
 * parent/child).
 */
export function SplitPanelSlot() {
  const { panel } = useSplitPanel();
  const [width, setWidth] = React.useState(panel?.width ?? 420);
  const wasOpenRef = React.useRef(false);
  const [isResizing, setIsResizing] = React.useState(false);
  const resizeStart = React.useRef<{ x: number; width: number } | null>(null);

  // Sync from the caller's requested width only on the closed -> open
  // transition, so an in-progress drag-resize survives openPanel() being
  // re-called with the same width while content refreshes (e.g. NoteEditor
  // re-pushes its panel content on every metadata edit).
  React.useEffect(() => {
    const isOpen = panel !== null;
    if (isOpen && !wasOpenRef.current) {
      setWidth(panel!.width);
    }
    wasOpenRef.current = isOpen;
  }, [panel]);

  React.useEffect(() => {
    if (!isResizing) {
      return;
    }
    function onMove(e: PointerEvent) {
      if (!resizeStart.current) {
        return;
      }
      const delta = resizeStart.current.x - e.clientX;
      setWidth(
        Math.min(
          MAX_PANEL_WIDTH,
          Math.max(MIN_PANEL_WIDTH, resizeStart.current.width + delta),
        ),
      );
    }
    function onUp() {
      resizeStart.current = null;
      setIsResizing(false);
    }
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    return () => {
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
  }, [isResizing]);

  function onResizePointerDown(e: React.PointerEvent) {
    e.preventDefault();
    resizeStart.current = { x: e.clientX, width };
    setIsResizing(true);
  }

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ width: 0 }}
        animate={{ width: panel ? width : 0 }}
        transition={isResizing ? { duration: 0 } : { duration: 0.22, ease: "easeInOut" }}
        className="relative flex min-h-0 shrink-0 flex-col overflow-hidden"
      >
        {panel ? (
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize panel"
            onPointerDown={onResizePointerDown}
            className="group absolute inset-y-0 left-0 z-10 flex w-2 cursor-col-resize items-center justify-center"
          >
            <div
              className={cn(
                "h-full w-px bg-border transition-colors",
                isResizing ? "bg-ring" : "group-hover:bg-ring",
              )}
            />
          </div>
        ) : null}
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
