import * as React from "react";
import { AnimatePresence, LazyMotion, domAnimation, m } from "framer-motion";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useResizableSize } from "@/workbench/useResizableSize";
import { GAP_WIDTH, maxPanelWidth, useWindowWidth } from "@/workbench/panelLayout";
import { SPLIT_MIN_HEIGHT, splitColumn } from "@/workbench/splitColumn";
import { usePanelWidthsStore } from "@/stores/panelWidths";
import { ShellResizeHandle } from "@/components/ui/resize-handle";
import { cn } from "@/lib/utils";
import { CONTENT_FADE_DELAY, EASE_OUT_STRONG, MOTION_BASE, MOTION_FAST, MOTION_SLOW } from "@/lib/motion";

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
 * The right column, shared by two stacked slots.
 *
 * `panel` belongs to the active module and dies with it. `companion` belongs to
 * the shell and survives module switches, which is where a conversation has to
 * live: a module's subtree unmounts the moment you leave it (see
 * modules/codegraph/codegraphStore.ts).
 *
 * Width is shared by both. Only the split between them is separately dragged.
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

const MIN_PANEL_WIDTH = 320;
const MAX_PANEL_WIDTH = 720;


/** One slot's card: rounded surface, close button, fading content. */
function PanelCard({
  state,
  className,
  style,
  label,
}: {
  state: PanelState;
  className?: string;
  style?: React.CSSProperties;
  label: string;
}) {
  return (
    <div
      style={style}
      className={cn(
        // Figma panel-surface 116:1348 is surface/base at full opacity with a
        // glass/border hairline and radius/10 — the same material as the content
        // card, not a glass tier. It was glass-panel (55%) at radius 12.
        "relative flex min-h-0 flex-col overflow-hidden shell-surface",
        className,
      )}
    >
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label={label}
        className="absolute top-2 right-2 z-10"
        onClick={state.onClose}
      >
        <X className="size-4" />
      </Button>
      <AnimatePresence>
        {/* The body follows the container rather than arriving with it
            (smoothness 4.4): the panel slides, then its content fades up 60 ms
            behind — which is what makes the panel read as bringing something
            in rather than blinking into place. Leaving is the reverse and
            faster, with no delay to wait through. */}
        <m.div
          key="panel-content"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0, transition: { duration: MOTION_FAST, delay: 0 } }}
          transition={{ duration: MOTION_BASE, delay: CONTENT_FADE_DELAY, ease: EASE_OUT_STRONG }}
          className="flex min-h-0 flex-1 flex-col overflow-auto"
        >
          {state.content}
        </m.div>
      </AnimatePresence>
    </div>
  );
}

/**
 * Renders as a flex sibling of the active module's content (see
 * Workbench.tsx). Its own LazyMotion wrap is required — it is not a
 * descendant of Dock's LazyMotion (siblings under Workbench, not
 * parent/child).
 */
export function SplitPanelSlot() {
  const { panel, companion } = useSplitPanel();
  const width = usePanelWidthsStore((s) => s.splitPanelWidth);
  const setWidth = usePanelWidthsStore((s) => s.setSplitPanelWidth);
  const topHeight = usePanelWidthsStore((s) => s.splitTopHeight);
  const setTopHeight = usePanelWidthsStore((s) => s.setSplitTopHeight);
  const setSplitPanelOpen = usePanelWidthsStore((s) => s.setSplitPanelOpen);
  const sidebarWidth = usePanelWidthsStore((s) => s.sidebarWidth);
  const sidebarOpen = usePanelWidthsStore((s) => s.sidebarOpen);
  const windowWidth = useWindowWidth();
  const wasOpenRef = React.useRef(false);

  const open = panel !== null || companion !== null;
  const bothOpen = panel !== null && companion !== null;

  // Measured, not derived: the column's height depends on the title bar, the
  // bottom slot and any padding above it, and re-deriving that sum here would
  // drift the moment one of them changes.
  const columnRef = React.useRef<HTMLDivElement | null>(null);
  const [columnHeight, setColumnHeight] = React.useState(0);
  React.useLayoutEffect(() => {
    const el = columnRef.current;
    if (!el) return;
    setColumnHeight(el.getBoundingClientRect().height);
    const observer = new ResizeObserver((entries) => {
      setColumnHeight(entries[0].contentRect.height);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // Publish this panel's occupancy so SidebarSurface can clamp against it.
  React.useEffect(() => {
    setSplitPanelOpen(open);
  }, [open, setSplitPanelOpen]);

  // Sync from the caller's requested width only on the closed -> open
  // transition, so an in-progress drag-resize survives openPanel() being
  // re-called with the same width while content refreshes (e.g. NoteEditor
  // re-pushes its panel content on every metadata edit).
  React.useEffect(() => {
    if (panel !== null && !wasOpenRef.current) {
      setWidth(panel.width);
    }
    wasOpenRef.current = panel !== null;
  }, [panel, setWidth]);

  // Clamped against the sidebar, not just a flat ceiling: with both panels
  // open on a narrow window, a flat 720 here would crush the centre content
  // area straight through its floor. Hoisted out of the hook call because the
  // rendered width needs the same ceiling the drag does.
  const max = maxPanelWidth({
    windowWidth,
    otherEffectiveWidth: sidebarOpen ? sidebarWidth : 0,
    min: MIN_PANEL_WIDTH,
    hardMax: MAX_PANEL_WIDTH,
  });

  const { isResizing, onPointerDown } = useResizableSize({
    size: width,
    onSizeChange: setWidth,
    min: MIN_PANEL_WIDTH,
    max,
    axis: "x",
    sign: -1,
  });

  const sizes = splitColumn({
    columnHeight,
    topOpen: panel !== null,
    bottomOpen: companion !== null,
    topHeight,
  });

  // sign +1: the handle sits under the top slot, so dragging down grows it.
  const splitDrag = useResizableSize({
    size: topHeight,
    onSizeChange: setTopHeight,
    min: SPLIT_MIN_HEIGHT,
    max: Math.max(SPLIT_MIN_HEIGHT, columnHeight - GAP_WIDTH - SPLIT_MIN_HEIGHT),
    axis: "y",
    sign: 1,
  });

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ width: 0 }}
        // Clamped at render for the same reason as the sidebar and the bottom
        // slot: the ceiling moves with the window, the stored width does not.
        // One line on purpose: step4bComponents.test.tsx finds the sizing
        // animation by scanning for a line carrying both animate={{ and
        // GAP_WIDTH, and asserts it clamps with Math.min.
        animate={{ width: open ? Math.min(width, max) + GAP_WIDTH : 0 }}
        // Closing is faster than opening, everywhere (smoothness 4.1).
        transition={
          isResizing
            ? { duration: 0 }
            : { duration: open ? MOTION_SLOW : MOTION_FAST, ease: EASE_OUT_STRONG }
        }
className="flex min-h-0 shrink-0 overflow-hidden"
      >
        {open ? (
          <ShellResizeHandle
            orientation="vertical"
            aria-label="Resize panel"
            onPointerDown={onPointerDown}
            isResizing={isResizing}
            style={{ width: GAP_WIDTH }}
          />
        ) : null}

        <div ref={columnRef} className="flex min-h-0 flex-1 flex-col overflow-hidden">
          {panel ? (
            <PanelCard
              state={panel}
              label="Close panel"
              className={bothOpen ? "shrink-0" : "flex-1"}
              style={bothOpen ? { height: sizes.top } : undefined}
            />
          ) : null}

          {bothOpen ? (
            <ShellResizeHandle
              orientation="horizontal"
              aria-label="Resize panel split"
              onPointerDown={splitDrag.onPointerDown}
              isResizing={splitDrag.isResizing}
              style={{ height: GAP_WIDTH }}
            />
          ) : null}

          {companion ? (
            <PanelCard
              state={companion}
              label="Close conversation"
              className={bothOpen ? "shrink-0" : "flex-1"}
              style={bothOpen ? { height: sizes.bottom } : undefined}
            />
          ) : null}
        </div>
      </m.div>
    </LazyMotion>
  );
}
