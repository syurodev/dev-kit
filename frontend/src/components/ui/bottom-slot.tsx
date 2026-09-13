import * as React from "react";
import { LazyMotion, domAnimation, m } from "framer-motion";
import { useResizableSize } from "@/workbench/useResizableSize";
import {
  GAP_WIDTH,
  maxBottomHeight,
  useWindowHeight,
} from "@/workbench/panelLayout";
import { useBottomContentStore } from "@/stores/bottomContent";
import { useBottomSlotStore } from "@/stores/bottomSlot";
import { TerminalWorkspace } from "@/workbench/TerminalWorkspace";
import { SidebarModuleIdContext } from "@/components/ui/sidebar-shell";
import { ShellResizeHandle } from "@/components/ui/resize-handle";

/**
 * Registers `content` as the calling module's bottom-slot panel for as long as
 * the component stays mounted. Registration is independent of whether the slot
 * is currently shown — that is a global toggle (useBottomSlotStore), so a
 * module registers unconditionally and the shell decides visibility.
 *
 * Reuses SidebarModuleIdContext rather than a second identical context: both
 * slots ask "which module is this subtree", and WorkbenchViews already provides
 * it once per module subtree.
 *
 * Deliberately reads only the store's setter: see stores/bottomContent.ts for
 * why subscribing to the content itself here would deadlock the render loop.
 */
export function useBottomContent(content: React.ReactNode | null) {
  const moduleId = React.useContext(SidebarModuleIdContext);
  const setContent = useBottomContentStore((s) => s.setContent);

  React.useEffect(() => {
    if (!moduleId) return;
    setContent(moduleId, content);
    return () => setContent(moduleId, null);
  }, [moduleId, content, setContent]);
}

const MIN_BOTTOM_HEIGHT = 120;
const MAX_BOTTOM_HEIGHT = 600;

/**
 * The rendered bottom surface. Stacked directly under WorkbenchViews inside
 * the module-content column (see Workbench.tsx), so it is exactly as wide as
 * the module content — the sidebar and the split panel run full height either
 * side of it and opening this never shortens them.
 *
 * That is a deliberate departure from the Figma App Shell's Bottom=Open
 * variant, which draws the bottom surface spanning the full window width and
 * cutting across the sidebar. The vertical extent is unchanged.
 */
export function BottomSlotSurface({ activeModuleId }: { activeModuleId: string | null }) {
  const open = useBottomSlotStore((s) => s.open);
  const height = useBottomSlotStore((s) => s.height);
  const setHeight = useBottomSlotStore((s) => s.setHeight);
  const windowHeight = useWindowHeight();

  const content = useBottomContentStore((s) =>
    activeModuleId ? s.content[activeModuleId] : undefined,
  );

  const max = maxBottomHeight({
    windowHeight,
    min: MIN_BOTTOM_HEIGHT,
    hardMax: MAX_BOTTOM_HEIGHT,
  });

  const { isResizing, onPointerDown } = useResizableSize({
    size: height,
    onSizeChange: setHeight,
    min: MIN_BOTTOM_HEIGHT,
    max,
    axis: "y",
    // The handle sits on this panel's top edge, so dragging up grows it.
    sign: -1,
  });

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ height: 0 }}
        // Clamped at render, not only while dragging: the clamp depends on the
        // window, and the window can shrink long after the drag ended. Without
        // this, dragging to 600px on a tall window and then making the window
        // short would leave a shrink-0 surface taller than the space available
        // and crush the content row above it to nothing. The *stored* height
        // stays unclamped on purpose, so widening the window back restores the
        // size the user actually chose.
        //
        // SidebarSurface has the same hole on the horizontal axis today. Not
        // fixed here — it is a separate change to a file this plan otherwise
        // only renames an import in.
        animate={{ height: open ? Math.min(height, max) + GAP_WIDTH : 0 }}
        transition={isResizing ? { duration: 0 } : { duration: 0.22, ease: "easeInOut" }}
        className="flex min-w-0 shrink-0 flex-col overflow-hidden"
      >
        {open ? (
          <ShellResizeHandle
            orientation="horizontal"
            aria-label="Resize bottom panel"
            onPointerDown={onPointerDown}
            isResizing={isResizing}
            style={{ height: GAP_WIDTH }}
          />
        ) : null}
        {/* Same treatment as SplitPanelSlot's own box — a raised content card,
            not the recessed bg-sidebar rail. A terminal or a task log reads as
            content, not as navigation chrome. */}
        {/* Figma bottom-surface 204:462: surface/base, glass/border, radius/10 —
            identical to the content card and the side panel. */}
        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden shell-surface">
          {/* The workspace owns this surface: terminals belong to the shell so
              they survive a module switch, the way a build running in one must.
              Module-registered content still gets a tab beside them. */}
          <TerminalWorkspace moduleContent={content ?? undefined} />
</div>
      </m.div>
    </LazyMotion>
  );
}
