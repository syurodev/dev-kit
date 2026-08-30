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
          className={`size-3 text-text-tertiary transition-transform ${open ? "" : "-rotate-90"}`}
        />
      )}
      <span className="text-[10px] font-semibold uppercase tracking-[0.08em] text-text-tertiary group-hover:text-text-secondary">
        {label}
      </span>
      {count !== undefined && (
        <span className="text-[10px] font-mono text-text-tertiary/70">{count}</span>
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
      className={`group flex items-center gap-2 rounded-lg px-3 py-2 text-[13px] ${
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
        <span className="text-[11px] text-text-tertiary">{count}</span>
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
        animate={{ width: open ? sidebarWidth + GAP_WIDTH : 0 }}
        transition={isResizing ? { duration: 0 } : { duration: 0.22, ease: "easeInOut" }}
        className="flex min-h-0 shrink-0 overflow-hidden"
      >
        {/* Deliberately NOT the glass-panel treatment SplitPanelSlot and the
            content card use: the sidebar reads as a recessed rail, not a third
            card. glass-rail is the most transparent material — borderless,
            near-clear tint so the native vibrancy backdrop carries it, the way
            Finder/Notes sidebars sit closest to the desktop. */}
        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl glass-rail">
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
