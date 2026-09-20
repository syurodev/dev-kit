import * as React from "react";
import { ChevronDown, Search, X } from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { LazyMotion, domAnimation, m } from "framer-motion";
import { useResizableSize } from "@/workbench/useResizableSize";
import {
  GAP_WIDTH,
  maxPanelWidth,
  useWindowWidth,
} from "@/workbench/panelLayout";
import { usePanelWidthsStore } from "@/stores/panelWidths";
import { useSidebarContentStore } from "@/stores/sidebarContent";
import { ModuleSidebar } from "./module-sidebar";
import { useSidebarOpenStore } from "@/stores/sidebarShell";
import { ShellResizeHandle } from "@/components/ui/resize-handle";
import { useLayoutPressure } from "@/workbench/useLayoutPressure";
import { armBeside } from "@/stores/workbenchTabs";

/**
 * Identifies which module a mounted view belongs to. Provided once per module
 * subtree by WorkbenchViews; its value never changes for a given subtree, so
 * consuming it costs no extra renders.
 */
export const SidebarModuleIdContext = React.createContext<string | null>(null);

/**
 * SidebarSurface re-renders whenever any module registers a rail; a background
 * module's own rail must not re-render with it. Its only prop is a string.
 */
const MemoModuleSidebar = React.memo(ModuleSidebar);

/**
 * Layout effect, not effect — the rail must be in the same paint as the body
 * that registered it (smoothness 2.5). A passive effect writes the store after
 * the commit and React flushes passive work in a separate task, leaving the
 * browser free to paint the module without its rail. Measured in headless
 * Chromium: the rail trails the body by a frame in 4 of 5 runs at a 14ms commit
 * and 5 of 5 at 20ms — a real module mount — but not under a trivial one, which
 * is why it read as intermittent. With the layout effect the delta is 0.
 *
 * Guarded for SSR: `useLayoutEffect` warns under renderToString, and several
 * module views are rendered that way in tests.
 */
const useIsomorphicLayoutEffect =
  typeof window === "undefined" ? React.useEffect : React.useLayoutEffect;

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
  // Identifies this caller for as long as it is mounted. A module with several
  // documents open has several of them registering the same rail, and an
  // unconditional clear on unmount emptied it for the ones still on screen —
  // closing one Database tab blanked the rail under the others.
  const registrar = React.useId();

  useIsomorphicLayoutEffect(() => {
    if (!moduleId) return;
    setContent(moduleId, content, registrar);
    return () => setContent(moduleId, null, registrar);
  }, [moduleId, content, setContent, registrar]);
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
  first = false,
}: {
  label: string;
  count?: number;
  open?: boolean;
  onToggle?: () => void;
  actions?: React.ReactNode;
  /**
   * The first heading in a rail that begins with one.
   *
   * pt-4 is the air a heading needs when it follows rows. A heading that
   * follows the rail's own chrome — a tab strip, a search field — already
   * has that air above it, and 16px more reads as the list having failed to
   * load. 6px is what the search field leaves, so both panes of a tabbed
   * rail start on the same line.
   */
  first?: boolean;
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
    // pt-4/pb-1.5, not a symmetric py: a section heading belongs to the rows
    // below it, and equal padding leaves it floating between two groups.
    <div
      className={cn(
        "flex items-center justify-between gap-2 px-3 pb-1.5",
        first ? "pt-2" : "pt-4",
      )}
    >
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

/**
 * A rail row's height follows the SHAPE of what it holds, not the module it is
 * in — so the three tiers are the whole vocabulary and every rail picks from it:
 *
 *   tree  24  a deep tree, where depth costs vertical room (Database's five
 *             levels over 173 tables, Kafka, CodeGraph)
 *   list  32  one line of label, the default (Tools, Notes)
 *   card  48  two lines, a title over its meta (Sync, Agent Hub)
 *
 * Everything else about a row is identical across all three: rounded-md, px-3,
 * gap-2, and surface-raised when active.
 */
export const SIDEBAR_ROW_HEIGHT = {
  tree: "h-(--control-height-xs)",
  list: "h-(--control-height-md)",
  card: "h-(--control-height-2xl)",
} as const;

export type SidebarRowTier = keyof typeof SIDEBAR_ROW_HEIGHT;

/** One selectable line in a rail: icon, label, trailing count. */
export function SidebarRow({
  icon,
  label,
  count,
  active,
  onClick,
  trailing,
  tier = "list",
}: {
  icon?: React.ReactNode;
  label: React.ReactNode;
  count?: number;
  active?: boolean;
  onClick?: () => void;
  trailing?: React.ReactNode;
  tier?: SidebarRowTier;
}) {
  return (
    <div
      // Metrics from Foundations, structure from the component. The Figma
      // sidebar draws 31px rows with 7/12 padding and a 9px gap, which the
      // Foundations page itself rules out — "spacing tokens drive auto-layout
      // gaps and padding, avoid arbitrary values". Rounded onto the grid:
      // 12/8 padding, 8 gap, radius 8 (controls are 6–10).
      // A fixed height, not vertical padding: the trailing control is 24px and
      // the label's line box is 17, so whichever is taller would decide the
      // row's height — and a control that only appears on hover would decide it
      // twice. The tier token is the height; nothing inside can move it.
      className={cn(
        "group flex items-center gap-2 rounded-md px-3 text-body-13",
        SIDEBAR_ROW_HEIGHT[tier],
        active
          ? "bg-surface-raised text-text-primary"
          : "text-text-secondary hover:bg-surface-raised",
      )}
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

/**
 * The rail's search field. Four rails had grown their own — two at 28px with a
 * 12px glyph, one at 32px with a 14px one, one with no styling at all — and a
 * search box is the first thing in a rail, so the drift was visible on every
 * module switch.
 *
 * `onClear` opts into the trailing clear button; without it the right padding
 * is not reserved, because an X that never appears is 28px of dead field.
 */
export function SidebarSearch({
  value,
  onChange,
  onClear,
  placeholder,
  "aria-label": ariaLabel,
}: {
  value: string;
  onChange: (value: string) => void;
  onClear?: () => void;
  placeholder: string;
  "aria-label"?: string;
}) {
  return (
    <div className="relative min-w-0 flex-1">
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-(--icon-14) -translate-y-1/2 text-text-tertiary" />
      <Input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        aria-label={ariaLabel ?? placeholder}
        className={cn(
          "h-(--control-height-md) border-border-subtle bg-surface-well pl-8 text-body-13",
          onClear && "pr-7",
        )}
      />
      {onClear && value ? (
        <button
          type="button"
          onClick={onClear}
          title="Clear search"
          className="absolute top-1/2 right-1.5 -translate-y-1/2 rounded-md p-0.5 text-text-tertiary hover:text-text-primary"
        >
          <X className="size-(--icon-12)" />
        </button>
      ) : null}
    </div>
  );
}

/**
 * The band a rail's search field and its adjacent actions sit in. One padding
 * for every rail, so the first row below it starts at the same y everywhere.
 */
export function SidebarHeader({ children }: { children: React.ReactNode }) {
  return <div className="flex items-center gap-1 pt-1.5 pb-1">{children}</div>;
}

const MIN_SIDEBAR_WIDTH = 240;
const MAX_SIDEBAR_WIDTH = 480;

/**
 * The rendered column. Flex sibling to the left of WorkbenchViews (see
 * Workbench.tsx) — mirrors SplitPanelSlot's box-in-box shape (own
 * rounded-xl/border/bg-card, not a shared card with an internal divider).
 */
export function SidebarSurface({ activeModuleId }: { activeModuleId: string | null }) {
  const wantOpen = useSidebarOpenStore((s) => s.open);
  // Too narrow for everything open, the sidebar is the first to give way — its
  // open state untouched, so it comes back as the window widens
  // (panelLayout.layoutPressure).
  const pressure = useLayoutPressure();
  const open = wantOpen && !pressure.sidebarYields;
  const sidebarWidth = usePanelWidthsStore((s) => s.sidebarWidth);
  const setSidebarWidth = usePanelWidthsStore((s) => s.setSidebarWidth);
  const columnWidths = usePanelWidthsStore((s) => s.columnWidths);
  const windowWidth = useWindowWidth();

  const registered = useSidebarContentStore((s) => s.content);
  // Every module visited this session keeps its rail mounted, hidden while
  // another is on screen (sidebar.mdx, "Ẩn chứ không huỷ sau lần mở đầu").
  // Only the active module's rail used to render, so switching back to one
  // rebuilt it from nothing: Database's is a non-virtualised tree plus five IPC
  // calls on mount, measured at ~300ms of React commit for a 900-row schema on
  // every single switch. The cost now is memory per visited module, on purpose.
  const [visited, setVisited] = React.useState<string[]>([]);
  if (activeModuleId && !visited.includes(activeModuleId)) {
    setVisited([...visited, activeModuleId]);
  }

  const max = maxPanelWidth({
    windowWidth,
    // The columns keep their last width in the store while closed; only the
    // space they actually occupy may shrink this panel's ceiling. Each drawn
    // width carries its sash, and maxPanelWidth already counts one.
    otherEffectiveWidth: Math.max(0, columnWidths.a + columnWidths.b - GAP_WIDTH),
    contentMin: pressure.contentMin,
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
        {/* Keep the shared titlebar background visible through the rail.

            No inset of its own, on any side. A rail starts where the content
            card starts and runs to the edge of the window on the left; what
            sits between a rail's rows is the rail's to decide, not the
            shell's. Every module's list also scrolls inside this wrapper, so an
            inset on the right would have put each rail's scrollbar that far in
            from its edge, with the 8px sash beyond it — a scrollbar belongs
            against the edge it scrolls. */}
        {/* ⌘-click (Ctrl-click) on anything in a rail opens what it opens
            beside the focused group rather than in it. Armed on the way down,
            consumed by the openTab that click makes — no rail had to change. */}
        <div
          className="relative flex min-h-0 flex-1 flex-col overflow-hidden glass-rail"
          onClickCapture={(e) => {
            if (e.metaKey || e.ctrlKey) armBeside();
          }}
        >
          {visited.map((moduleId) => (
            // `contents` keeps the rail a direct flex child of the column, as
            // it was before it had a wrapper; `hidden` takes a background one
            // out of layout, focus and the accessibility tree.
            <div key={moduleId} className={moduleId === activeModuleId ? "contents" : "hidden"}>
              {/* A document's rail wins where there is one; the module's own is the floor. */}
              {registered[moduleId] ?? <MemoModuleSidebar moduleId={moduleId} />}
            </div>
          ))}
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
