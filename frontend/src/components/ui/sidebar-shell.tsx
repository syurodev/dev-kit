import * as React from "react";
import { LazyMotion, domAnimation, m } from "framer-motion";
import { useResizableSize } from "@/workbench/useResizableSize";
import {
  GAP_WIDTH,
  maxPanelWidth,
  useWindowWidth,
} from "@/workbench/panelLayout";
import { usePanelWidthsStore } from "@/stores/panelWidths";
import { useSidebarContentStore } from "@/stores/sidebarContent";
import { useSidebarOpenStore } from "@/stores/sidebarShell";

/**
 * Identifies which module a mounted view belongs to. Provided once per module
 * subtree by WorkbenchViews; its value never changes for a given subtree, so
 * consuming it costs no extra renders.
 */
export const SidebarModuleIdContext = React.createContext<string | null>(null);

/**
 * Registers `content` as the calling module's sidebar for as long as the
 * component stays mounted. Registration is independent of whether the sidebar
 * is currently shown — that is a global toggle (useSidebarOpenStore), so a
 * module registers unconditionally and the shell decides visibility.
 *
 * Deliberately reads only the store's setter: see stores/sidebarContent.ts for
 * why subscribing to the content itself here would deadlock the render loop.
 */
export function useSidebarContent(content: React.ReactNode | null) {
  const moduleId = React.useContext(SidebarModuleIdContext);
  const setContent = useSidebarContentStore((s) => s.setContent);

  React.useEffect(() => {
    if (!moduleId) return;
    setContent(moduleId, content);
    return () => setContent(moduleId, null);
  }, [moduleId, content, setContent]);
}

const MIN_SIDEBAR_WIDTH = 240;
const MAX_SIDEBAR_WIDTH = 480;

/**
 * The rendered column. Flex sibling to the left of WorkbenchViews (see
 * Workbench.tsx) — mirrors SplitPanelSlot's box-in-box shape (own
 * rounded-xl/border/bg-card, not a shared card with an internal divider).
 */
export function SidebarSurface({ activeModuleId }: { activeModuleId: string | null }) {
  const open = useSidebarOpenStore((s) => s.open);
  const sidebarWidth = usePanelWidthsStore((s) => s.sidebarWidth);
  const setSidebarWidth = usePanelWidthsStore((s) => s.setSidebarWidth);
  const splitPanelWidth = usePanelWidthsStore((s) => s.splitPanelWidth);
  const splitPanelOpen = usePanelWidthsStore((s) => s.splitPanelOpen);
  const windowWidth = useWindowWidth();

  const content = useSidebarContentStore((s) =>
    activeModuleId ? s.content[activeModuleId] : undefined,
  );

  const max = maxPanelWidth({
    windowWidth,
    // The split panel keeps its last width in the store while closed; only the
    // space it is actually occupying may shrink this panel's ceiling.
    otherEffectiveWidth: splitPanelOpen ? splitPanelWidth : 0,
    min: MIN_SIDEBAR_WIDTH,
    hardMax: MAX_SIDEBAR_WIDTH,
  });

  const { isResizing, onPointerDown } = useResizableSize({
    size: sidebarWidth,
    onSizeChange: setSidebarWidth,
    min: MIN_SIDEBAR_WIDTH,
    max,
    axis: "x",
    sign: 1,
  });

  // Publish the sidebar's own occupancy so SplitPanelSlot can clamp against it.
  const setSidebarOpen = usePanelWidthsStore((s) => s.setSidebarOpen);
  React.useEffect(() => {
    setSidebarOpen(open);
  }, [open, setSidebarOpen]);

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ width: 0 }}
        animate={{ width: open ? sidebarWidth + GAP_WIDTH : 0 }}
        transition={isResizing ? { duration: 0 } : { duration: 0.22, ease: "easeInOut" }}
        className="flex min-h-0 shrink-0 overflow-hidden"
      >
        {/* Deliberately NOT the bg-card + border treatment SplitPanelSlot and
            the content card use. In the App Shell component in Figma,
            sidebar-surface is filled with background/app and has no stroke,
            while content-surface is surface/base with a border — the sidebar
            reads as a recessed dark rail against the raised content card, not
            as a third card. bg-sidebar is that token (--sidebar resolves to
            --background-app) and is what the vendored shadcn Sidebar in Notes
            already paints with. */}
        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl bg-sidebar">
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
              className={
                isResizing
                  ? "h-full w-px bg-ring transition-colors"
                  : "h-full w-px bg-transparent transition-colors group-hover:bg-border"
              }
            />
          </div>
        ) : null}
      </m.div>
    </LazyMotion>
  );
}
