import * as React from "react";
import { ChevronDown } from "lucide-react";
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
import { ShellResizeHandle } from "@/components/ui/resize-handle";

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

/**
 * Rail chrome, shared.
 *
 * Every module was hand-rolling its own section headers and rows, so the rails
 * drifted apart — different text sizes, different count styling, a collapse
 * chevron in one and not the other. The rail belongs to the shell, and so does
 * the shape of what goes in it.
 */
export function SidebarSection({
  label,
  count,
  open,
  onToggle,
  actions,
}: {
  label: string;
  count?: number;
  open?: boolean;
  onToggle?: () => void;
  actions?: React.ReactNode;
}) {
  const heading = (
    <>
      {onToggle && (
        <ChevronDown
          className={`size-(--icon-12) text-text-tertiary transition-transform ${open ? "" : "-rotate-90"}`}
        />
      )}
      <span className="text-label-11-caps text-text-tertiary group-hover:text-text-secondary">
        {label}
      </span>
      {count !== undefined && (
        <span className="text-code-11 text-text-tertiary/70">{count}</span>
      )}
    </>
  );

  return (
    <div className="flex items-center justify-between gap-2 px-3 py-2">
      {onToggle ? (
        <button
          type="button"
          onClick={onToggle}
          className="group flex min-w-0 items-center gap-1 text-left"
          title={open ? `Collapse ${label}` : `Expand ${label}`}
        >
          {heading}
        </button>
      ) : (
        <div className="group flex min-w-0 items-center gap-1">{heading}</div>
      )}
      {actions ? <div className="flex shrink-0 items-center gap-1">{actions}</div> : null}
    </div>
  );
}

/** One selectable line in a rail: icon, label, trailing count. */
export function SidebarRow({
  icon,
  label,
  count,
  active,
  onClick,
  trailing,
}: {
  icon?: React.ReactNode;
  label: React.ReactNode;
  count?: number;
  active?: boolean;
  onClick?: () => void;
  trailing?: React.ReactNode;
}) {
  return (
    <div
      // Metrics from Foundations, structure from the component. The Figma
      // sidebar draws 31px rows with 7/12 padding and a 9px gap, which the
      // Foundations page itself rules out — "spacing tokens drive auto-layout
      // gaps and padding, avoid arbitrary values". Rounded onto the grid: 32
      // tall, 12/8 padding, 8 gap, radius 8 (controls are 6–10).
      // A fixed height, not vertical padding: the trailing control is 24px and
      // the label's line box is 17, so whichever is taller was deciding the
      // row's height — and a control that only appears on hover decided it
      // twice. h-8 is control/height-md; nothing inside can move it.
      className={`group flex h-(--control-height-md) items-center gap-2 rounded-lg px-3 text-body-13 ${
        active
          ? "bg-surface-raised text-text-primary"
          : "text-text-secondary hover:bg-surface-raised"
      }`}
    >
      <button
        type="button"
        onClick={onClick}
        className="flex min-w-0 flex-1 items-center gap-2 text-left hover:text-text-primary"
      >
        {icon}
        <span className="truncate">{label}</span>
      </button>
      {count !== undefined && (
        <span className="text-caption-11 text-text-tertiary">{count}</span>
      )}
      {trailing}
    </div>
  );
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
        // Clamped at render, not only while dragging. The clamp depends on the
        // window, and the window can shrink long after the drag ended: drag to
        // 600 on a wide window, then narrow it, and this shrink-0 rail would
        // keep its 600 and crush the module column beside it. bottom-slot.tsx
        // fixed this on the vertical axis and left a note saying the sidebar
        // still had the hole; this is that note being paid. The *stored* width
        // stays unclamped, so widening the window restores the user's choice.
        animate={{ width: open ? Math.min(sidebarWidth, max) + GAP_WIDTH : 0 }}
        transition={isResizing ? { duration: 0 } : { duration: 0.22, ease: "easeInOut" }}
        className="flex min-h-0 shrink-0 overflow-hidden"
      >
        {/* Deliberately NOT the glass-panel treatment SplitPanelSlot and the
            content card use: the sidebar reads as a recessed rail, not a third
            card. glass-rail is the most transparent material — borderless,
            near-clear tint so the native vibrancy backdrop carries it, the way
            Finder/Notes sidebars sit closest to the desktop. */}
        {/* Figma sidebar-surface 116:1367 is the one docked surface with no
            material at all: no fill, no border, no radius — just p-2. Its
            description says the shell wrapper "owns the background/app fill
            shared with the titlebar", so the rail shows that through rather
            than painting its own. glass-rail resolves to transparent, which is
            why the fill was already right; the radius and the missing pad were
            not. The 8px pad is what makes a 280 rail hold 264 of content. */}
        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden p-2 glass-rail">
          {content}
        </div>
        {open ? (
          <ShellResizeHandle
            orientation="vertical"
            aria-label="Resize sidebar"
            onPointerDown={onPointerDown}
            isResizing={isResizing}
            style={{ width: GAP_WIDTH }}
          />
        ) : null}
      </m.div>
    </LazyMotion>
  );
}
