import * as React from "react";
import { LazyMotion, domAnimation, m } from "framer-motion";
import { useResizableWidth } from "@/workbench/useResizableWidth";
import { usePanelWidthsStore } from "@/stores/panelWidths";
import { useSidebarOpenStore } from "@/stores/sidebarShell";
import { cn } from "@/lib/utils";

export const SidebarModuleIdContext = React.createContext<string | null>(null);

type SidebarShellContextValue = {
  content: Map<string, React.ReactNode>;
  setContent: (moduleId: string, node: React.ReactNode | null) => void;
};

const SidebarShellContext = React.createContext<SidebarShellContextValue | null>(null);

export function SidebarProvider({ children }: { children: React.ReactNode }) {
  const [content, setContentState] = React.useState<Map<string, React.ReactNode>>(new Map());

  const setContent = React.useCallback((moduleId: string, node: React.ReactNode | null) => {
    setContentState((prev) => {
      const next = new Map(prev);
      if (node === null) {
        next.delete(moduleId);
      } else {
        next.set(moduleId, node);
      }
      return next;
    });
  }, []);

  const value = React.useMemo(() => ({ content, setContent }), [content, setContent]);

  return <SidebarShellContext.Provider value={value}>{children}</SidebarShellContext.Provider>;
}

/**
 * Registers `content` under the calling module's id (read from
 * SidebarModuleIdContext, provided once per mounted module view — see
 * Workbench.tsx). Registration is independent of whether the sidebar is
 * currently shown; that's controlled globally by useSidebarOpenStore, not by
 * this hook. Pass null to unregister explicitly (or just unmount — the
 * effect cleanup does the same thing).
 */
export function useSidebarContent(content: React.ReactNode | null) {
  const moduleId = React.useContext(SidebarModuleIdContext);
  const ctx = React.useContext(SidebarShellContext);
  if (!ctx) {
    throw new Error("useSidebarContent must be used within a SidebarProvider");
  }
  const { setContent } = ctx;

  React.useEffect(() => {
    if (!moduleId) return;
    setContent(moduleId, content);
    return () => setContent(moduleId, null);
  }, [moduleId, content, setContent]);
}

const MIN_SIDEBAR_WIDTH = 240;
const MAX_SIDEBAR_WIDTH = 480;
const CONTENT_MIN_WIDTH = 480;
const GAP_WIDTH = 4; // matches components/ui/split-panel.tsx's GAP_WIDTH — one gutter width, everywhere.

/**
 * The rendered column. Flex sibling to the left of WorkbenchViews (see
 * Workbench.tsx) — mirrors SplitPanelSlot's box-in-box shape (own
 * rounded-xl/border/bg-card, not a shared card with an internal divider).
 */
export function SidebarSurface({ activeModuleId }: { activeModuleId: string | null }) {
  const ctx = React.useContext(SidebarShellContext);
  if (!ctx) {
    throw new Error("SidebarSurface must be used within a SidebarProvider");
  }
  const open = useSidebarOpenStore((s) => s.open);
  const sidebarWidth = usePanelWidthsStore((s) => s.sidebarWidth);
  const setSidebarWidth = usePanelWidthsStore((s) => s.setSidebarWidth);
  const splitPanelWidth = usePanelWidthsStore((s) => s.splitPanelWidth);

  // windowWidth intentionally re-read on resize only, not on every render —
  // this clamp only matters while actively dragging, and useResizableWidth
  // already re-derives min/max on every pointermove via these props.
  const [windowWidth, setWindowWidth] = React.useState(() => window.innerWidth);
  React.useEffect(() => {
    const onResize = () => setWindowWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const max = Math.min(
    MAX_SIDEBAR_WIDTH,
    windowWidth - splitPanelWidth - CONTENT_MIN_WIDTH - GAP_WIDTH * 2,
  );

  const { isResizing, onPointerDown } = useResizableWidth({
    width: sidebarWidth,
    onWidthChange: setSidebarWidth,
    min: MIN_SIDEBAR_WIDTH,
    max,
    sign: 1,
  });

  const content = activeModuleId ? ctx.content.get(activeModuleId) : null;

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ width: 0 }}
        animate={{ width: open ? sidebarWidth + GAP_WIDTH : 0 }}
        transition={isResizing ? { duration: 0 } : { duration: 0.22, ease: "easeInOut" }}
        className="flex min-h-0 shrink-0 overflow-hidden"
      >
        <div
          className={cn(
            "relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border bg-card",
          )}
        >
          {content}
        </div>
        {open ? (
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize sidebar"
            onPointerDown={onPointerDown}
            style={{ width: GAP_WIDTH }}
            className="group flex shrink-0 cursor-col-resize items-center justify-center"
          >
            <div
              className={cn(
                "h-full w-px bg-transparent transition-colors",
                isResizing ? "bg-ring" : "group-hover:bg-border",
              )}
            />
          </div>
        ) : null}
      </m.div>
    </LazyMotion>
  );
}
