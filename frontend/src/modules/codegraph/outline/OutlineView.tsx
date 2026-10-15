import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { ChevronRight, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { TREE_ROW_HEIGHT, TREE_ROW_SELECTED, TreeGuides, treeContentX } from "@/components/tree-guides";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { useVirtualRows } from "@/components/ui/virtual-rows";
import { cn } from "@/lib/utils";
import type { CodeGraphNode } from "@/bridge/codegraph";
import type { FlowDoc } from "../codegraphStore";
import { loadSteps, openOutlineRows, setCallVisibility, toggleOutlineRow } from "../flowActions";
import { effectName } from "../graph/graphView";
import { BASE_HINT, RECURSION_HINT, SHOWN_ABOVE_HINT, effectHint, exitMarker, foldHint, hiddenHint, implsHint, shownHint } from "./outlineHints";
import { BRANCH_TEXT, ExitChip, FOLD_CHIP, Hint, MaybeHint, QUIET_MARK, implementationsLabel } from "./markers";
import { OUTLINE_LEGEND_CLEARANCE, OutlineLegend } from "./OutlineLegend";
import { EFFECT_CHIP } from "../graph/tokens";
import { buildOutline, type OutlineRow } from "./buildOutline";
import { EMPTY_KEYS, cursorIndex, isOpenRow, isRowOf, isToggleable, mergeKeys, newKeys, nodeOf, pathTo, treeShape } from "./outlineNav";

/** The sidebar tree's metrics, so the outline's guides and rows match it to the pixel. */
const ROW_HEIGHT = TREE_ROW_HEIGHT;
/** Space above the first row and below the last, carried by the spacers so the scroll maths sees it. */
const PAD_TOP = 4;
/** The tree's 8, plus room for the legend button, so the last row scrolls clear of it. */
const PAD_BOTTOM = 8 + OUTLINE_LEGEND_CLEARANCE;
/** Longer than a row's arrival (--motion-base) and its flash: when the classes stop mattering. */
const MOTION_SETTLED_MS = 1000;

export type OutlineViewProps = {
  folderId: string;
  root: string;
  flow: FlowDoc;
  nodes: CodeGraphNode[];
  selectedNodeId: string | null;
  onInspect: (nodeId: string) => void;
  onShowInGraph: (nodeId: string) => void;
  onOpenAt: (nodeId: string, line: number) => void;
};

/**
 * The Outline mode of a flow tab: the call flow read top to bottom, as the
 * code runs it. Rows come from buildOutline; this draws them, windows them,
 * and turns clicks, keys and the context menu into store actions.
 *
 * One tab stop: the tree holds focus and points at the cursor row with
 * aria-activedescendant. The cursor follows a row by key, so a row opening
 * above it does not move it onto another.
 */
export function OutlineView({ folderId, root, flow, nodes, selectedNodeId, onInspect, onShowInGraph, onOpenAt }: OutlineViewProps) {
  const byId = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);
  const rows = useMemo(
    () => (flow.steps ? buildOutline({ rootId: root, nodes: byId, steps: flow.steps, toggled: flow.toggled }) : []),
    [flow.steps, flow.toggled, root, byId],
  );
  const shape = useMemo(() => treeShape(rows), [rows]);
  // The row a jump landed on ("shown above", the selection) flashes once.
  const [flash, setFlash] = useState<{ key: string; n: number } | null>(null);
  const flashRow = (key: string) => setFlash((f) => ({ key, n: (f?.n ?? 0) + 1 }));
  useEffect(() => {
    if (!flash) return;
    const timer = setTimeout(() => setFlash((f) => (f === flash ? null : f)), MOTION_SETTLED_MS);
    return () => clearTimeout(timer);
  }, [flash]);
  const { scrollRef, indices, padTop, padBottom } = useVirtualRows(rows.length, ROW_HEIGHT);
  // Rows an opening just revealed arrive with a little motion: only those
  // drawn at that moment (a row further down, mounted later by a scroll, does
  // not play it), merged with any still arriving. Worked out against the rows
  // before, in state, so a re-render (or StrictMode's second one) sees the
  // same answer; cleared once the motion is over.
  const [arrived, setArrived] = useState<{ rows: typeof rows; fresh: ReadonlySet<string> }>({ rows, fresh: EMPTY_KEYS });
  let fresh = arrived.fresh;
  if (arrived.rows !== rows) {
    const drawn: [number, number] = [indices[0] ?? 0, indices[indices.length - 1] ?? -1];
    fresh = mergeKeys(arrived.fresh, newKeys(arrived.rows, rows, drawn));
    setArrived({ rows, fresh });
  }
  const arriving = arrived.fresh;
  useEffect(() => {
    if (arriving.size === 0) return;
    const timer = setTimeout(() => setArrived((a) => (a.fresh === arriving ? { rows: a.rows, fresh: EMPTY_KEYS } : a)), MOTION_SETTLED_MS);
    return () => clearTimeout(timer);
  }, [arriving]);
  const [cursorAt, setCursorAt] = useState<{ key: string | null; index: number }>({ key: null, index: 0 });
  const cursor = cursorIndex(rows, cursorAt.key, cursorAt.index);
  const setCursor = (index: number) => setCursorAt({ key: rows[index]?.key ?? null, index });
  // The cursor is drawn once the keyboard moves it and until the pointer is
  // used, which :focus-visible does not track reliably on a container.
  const [keyboardNav, setKeyboardNav] = useState(false);
  const [hasFocus, setHasFocus] = useState(false);
  const baseId = useId();
  const rowId = (index: number) => `${baseId}-row-${index}`;

  function scrollToIndex(index: number, center = false) {
    const el = scrollRef.current;
    if (!el) return;
    const top = PAD_TOP + index * ROW_HEIGHT;
    const bottom = top + ROW_HEIGHT;
    if (top >= el.scrollTop && bottom <= el.scrollTop + el.clientHeight) return;
    if (center) el.scrollTop = Math.max(0, top - el.clientHeight / 3);
    else el.scrollTop = top < el.scrollTop ? Math.max(0, top - PAD_TOP) : bottom + PAD_BOTTOM - el.clientHeight;
  }

  // The selection from the Graph (or the inspector) brings its first row into
  // view, opening the rows above it when the person's choices keep it closed.
  // Once per selection: the rows changing later (a row opened, a re-read)
  // must not jump the view back to it.
  const doneFor = useRef<string | null>(null);
  const triedFor = useRef<string | null>(null);
  useEffect(() => {
    if (!selectedNodeId) {
      doneFor.current = null;
      triedFor.current = null;
      return;
    }
    if (rows.length === 0 || doneFor.current === selectedNodeId) return;
    const index = rows.findIndex((r) => isRowOf(r, selectedNodeId));
    if (index >= 0) {
      doneFor.current = selectedNodeId;
      setCursorAt({ key: rows[index].key, index });
      scrollToIndex(index, true);
      flashRow(rows[index].key);
      return;
    }
    if (triedFor.current !== selectedNodeId && flow.steps) {
      triedFor.current = selectedNodeId;
      const keys = pathTo({ rootId: root, nodes: byId, steps: flow.steps, toggled: flow.toggled }, selectedNodeId);
      if (keys.length > 0) {
        openOutlineRows(folderId, root, keys);
        return;
      }
    }
    doneFor.current = selectedNodeId; // not in the outline at all
  }, [selectedNodeId, rows]); // only the selection and the rows decide; the rest is read

  function toggle(row: OutlineRow, open: boolean) {
    if (isToggleable(row)) toggleOutlineRow(folderId, root, row.key, open);
  }

  function activate(index: number) {
    const row = rows[index];
    if (!row) return;
    setCursor(index);
    if (row.kind === "fold") return toggle(row, !row.open);
    if ((row.kind === "call" || row.kind === "method") && row.ref) {
      // "shown above" and "recursion" lead to where that method is drawn.
      const target = rows.findIndex((r) => r.key === row.ref!.key);
      if (target >= 0) {
        setCursor(target);
        scrollToIndex(target, true);
        flashRow(rows[target].key);
      }
      return;
    }
    const id = nodeOf(row);
    if (!id) return;
    // Picked here, so it is already in view: the reveal that a selection made
    // in the graph gets (jump, scroll, flash) must not move the outline.
    doneFor.current = id;
    triedFor.current = id;
    onInspect(id);
  }

  function move(index: number) {
    const next = Math.max(0, Math.min(rows.length - 1, index));
    setCursor(next);
    scrollToIndex(next);
  }

  /** The context menu of the cursor row, from the keyboard: the trigger listens for contextmenu. */
  function openMenu(attempt = 0) {
    if (attempt === 0) scrollToIndex(cursor);
    const el = scrollRef.current?.querySelector<HTMLElement>(`[data-row-index="${cursor}"]`);
    if (!el) {
      // A windowed row scrolled to is mounted on the next frames.
      if (attempt < 3) requestAnimationFrame(() => openMenu(attempt + 1));
      return;
    }
    const rect = el.getBoundingClientRect();
    el.dispatchEvent(
      new MouseEvent("contextmenu", {
        bubbles: true,
        cancelable: true,
        clientX: rect.left + treeContentX(shape.level[cursor] - 1) + 24,
        clientY: rect.top + rect.height / 2,
      }),
    );
  }

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const row = rows[cursor];
    if (!row) return;
    switch (e.key) {
      case "ArrowDown":
        move(cursor + 1);
        break;
      case "ArrowUp":
        move(cursor - 1);
        break;
      case "Home":
        move(0);
        break;
      case "End":
        move(rows.length - 1);
        break;
      case "ArrowRight":
        if (isToggleable(row) && !isOpenRow(row)) toggle(row, true);
        else if (isOpenRow(row) && shape.parent[cursor + 1] === cursor) move(cursor + 1);
        break;
      case "ArrowLeft":
        if (isOpenRow(row)) toggle(row, false);
        else if (shape.parent[cursor] >= 0) move(shape.parent[cursor]);
        break;
      case "Enter":
      case " ":
        activate(cursor);
        break;
      case "ContextMenu":
        openMenu();
        break;
      case "F10":
        if (!e.shiftKey) return;
        openMenu();
        break;
      default:
        return;
    }
    e.preventDefault();
    setKeyboardNav(true);
  };

  const focusTree = () => scrollRef.current?.focus({ preventScroll: true });

  if (!flow.steps) {
    if (flow.stepsError) {
      return (
        <div className="flex flex-col items-start gap-3 p-5 text-body-13 text-status-danger">
          {flow.stepsError}
          <Button variant="outline" size="xs" className="bg-surface-raised" onClick={() => void loadSteps(folderId, root)}>
            Try again
            <RotateCcw className="size-3.5" />
          </Button>
        </div>
      );
    }
    return <p className="p-5 text-body-13 text-text-tertiary">Reading the flow…</p>;
  }

  const showCursor = keyboardNav && hasFocus;
  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {flow.stepsError && (
        // A re-read failed: the steps on screen stay, said to be the last ones read.
        <div className="flex shrink-0 items-center gap-3 border-b border-border-subtle px-4 py-2 text-caption-12 text-status-danger">
          <span className="min-w-0 flex-1 truncate" title={flow.stepsError}>
            Could not re-read the flow; showing the last steps read. {flow.stepsError}
          </span>
          <Button variant="outline" size="xs" className="bg-surface-raised" onClick={() => void loadSteps(folderId, root)}>
            Retry
            <RotateCcw className="size-3.5" />
          </Button>
        </div>
      )}
      {flow.steps.legacy && (
        <p className="shrink-0 border-b border-border-subtle px-4 py-2 text-caption-12 text-text-tertiary">
          This index was written before branches were recorded. Re-index to see them.
        </p>
      )}
      <div
        ref={scrollRef}
        data-scroll-box
        tabIndex={0}
        role="tree"
        aria-label="Flow outline"
        aria-activedescendant={rows.length > 0 && indices.includes(cursor) ? rowId(cursor) : undefined}
        onKeyDown={onKeyDown}
        onPointerDown={() => setKeyboardNav(false)}
        // The menu key's own contextmenu lands on the tree, not on a row:
        // send it to the cursor row instead of opening the browser's menu.
        onContextMenu={(e) => {
          if (e.target !== e.currentTarget) return;
          e.preventDefault();
          openMenu();
        }}
        onFocus={() => setHasFocus(true)}
        onBlur={() => setHasFocus(false)}
        className="min-h-0 flex-1 overflow-auto text-code-12 outline-none"
      >
        <div style={{ height: padTop + PAD_TOP }} />
        {indices.map((index) => (
          <OutlineLine
            key={rows[index].key}
            id={rowId(index)}
            index={index}
            row={rows[index]}
            level={shape.level[index]}
            lasts={shape.lasts[index]}
            hasChildren={shape.parent[index + 1] === index}
            posinset={shape.posinset[index]}
            setsize={shape.setsize[index]}
            entering={fresh.has(rows[index].key)}
            flash={flash?.key === rows[index].key ? flash.n : 0}
            selected={!!selectedNodeId && isRowOf(rows[index], selectedNodeId)}
            focused={showCursor && index === cursor}
            onActivate={() => activate(index)}
            onToggle={(open) => {
              setCursor(index);
              toggle(rows[index], open);
            }}
            onShowInGraph={onShowInGraph}
            onOpenAt={onOpenAt}
            onMenuClosed={focusTree}
          />
        ))}
        <div style={{ height: padBottom + PAD_BOTTOM }} />
      </div>
      <div className="absolute bottom-3 left-3 z-10">
        <OutlineLegend />
      </div>
    </div>
  );
}

function OutlineLine({
  id,
  index,
  row,
  level,
  lasts,
  hasChildren,
  posinset,
  setsize,
  entering,
  flash,
  selected,
  focused,
  onActivate,
  onToggle,
  onShowInGraph,
  onOpenAt,
  onMenuClosed,
}: {
  id: string;
  index: number;
  row: OutlineRow;
  /** aria-level: 1 for the root. */
  level: number;
  lasts: boolean[];
  /** Rows hang off this one, a frame heading's steps included: a guide runs down from it. */
  hasChildren: boolean;
  posinset: number;
  setsize: number;
  /** Revealed by an opening just now: it slides in. */
  entering: boolean;
  /** Non-zero while a jump's flash plays; a new number replays it. */
  flash: number;
  selected: boolean;
  focused: boolean;
  onActivate: () => void;
  onToggle: (open: boolean) => void;
  onShowInGraph: (nodeId: string) => void;
  onOpenAt: (nodeId: string, line: number) => void;
  onMenuClosed: () => void;
}) {
  const toggleable = isToggleable(row);
  const open = isOpenRow(row);
  const treeLevel = level - 1;
  const line = (
    <div
      id={id}
      data-row-index={index}
      role="treeitem"
      aria-level={level}
      aria-posinset={posinset}
      aria-setsize={setsize}
      aria-selected={selected}
      aria-expanded={toggleable ? open : hasChildren ? true : undefined}
      onClick={onActivate}
      className="relative"
      style={{ height: ROW_HEIGHT }}
    >
      <TreeGuides level={treeLevel} lasts={lasts} open={hasChildren} leaf={!toggleable} rowHeight={ROW_HEIGHT} />
      {/* The highlight starts at the row's chevron, as in the sidebar tree:
          a full-width one would paint over the guides it hangs from. Isolated,
          so the flash below sits over its background and under its text. */}
      <div
        className={cn(
          "absolute inset-y-0 right-1 isolate flex items-center gap-1.5 rounded-md border pr-1.5 whitespace-nowrap",
          selected ? TREE_ROW_SELECTED : "border-transparent hover:bg-state-soft",
          focused && "outline-1 -outline-offset-1 outline-border-focus/50",
          entering && "outline-row-enter",
        )}
        style={{ left: treeContentX(treeLevel) - 2, paddingLeft: "var(--space-tree-guide)" }}
      >
        {/* Keyed by the flash, so a second jump to the same row replays it
            without remounting the row's content. */}
        {flash > 0 && <span key={flash} aria-hidden className="outline-row-flash" />}
        <span
          className="flex size-3 shrink-0 items-center justify-center text-text-tertiary"
          onClick={(e) => {
            if (!toggleable) return;
            e.stopPropagation();
            onToggle(!open);
          }}
        >
          {toggleable && (
            <ChevronRight
              className={cn(
                "size-3 transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none",
                open && "rotate-90",
              )}
            />
          )}
        </span>
        <RowBody row={row} />
      </div>
    </div>
  );

  if (row.kind !== "call" && row.kind !== "method") return line;
  const nodeId = nodeOf(row)!;
  // Callee keys split at their first "#" into type and member, as Go reads them for "<Type>#*".
  const key = row.kind === "call" ? row.step.calleeKey : "";
  const hash = key.indexOf("#");
  const type = hash > 0 ? key.slice(0, hash) : "";
  const member = hash > 0 ? key.slice(hash + 1) : "";
  const name = simpleName(type);
  const at = row.kind === "call" ? row.step.line : row.line;
  const ownerId = row.kind === "call" ? row.ownerId : row.nodeId;
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{line}</ContextMenuTrigger>
      <ContextMenuContent
        onCloseAutoFocus={(e) => {
          // Back to the tree, which holds the cursor, rather than to the body.
          e.preventDefault();
          onMenuClosed();
        }}
      >
        {type && member && (
          <>
            <ContextMenuLabel className="max-w-80 truncate font-mono">{key}</ContextMenuLabel>
            <ContextMenuItem onSelect={() => void setCallVisibility(key, "show")}>Always show {name}.{member}</ContextMenuItem>
            <ContextMenuItem onSelect={() => void setCallVisibility(key, "hide")}>Always hide {name}.{member}</ContextMenuItem>
            <ContextMenuItem onSelect={() => void setCallVisibility(`${type}#*`, "show")}>Always show every call on {name}</ContextMenuItem>
            <ContextMenuItem onSelect={() => void setCallVisibility(`${type}#*`, "hide")}>Always hide every call on {name}</ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem onSelect={() => void setCallVisibility(key, "")}>Reset {name}.{member} to default</ContextMenuItem>
            <ContextMenuItem onSelect={() => void setCallVisibility(`${type}#*`, "")}>Reset every call on {name}</ContextMenuItem>
            <ContextMenuSeparator />
          </>
        )}
        <ContextMenuItem onSelect={() => onOpenAt(ownerId, at)}>Open in editor</ContextMenuItem>
        <ContextMenuItem onSelect={() => onShowInGraph(nodeId)}>Show in Graph</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

const simpleName = (type: string) => type.slice(type.lastIndexOf(".") + 1);

function RowBody({ row }: { row: OutlineRow }) {
  switch (row.kind) {
    case "method":
      return (
        <>
          {row.impl && <span className="text-text-tertiary">↳</span>}
          <span className="font-semibold text-text-primary">{row.title}</span>
          {row.base && (
            <Hint text={BASE_HINT}>
              <span className={QUIET_MARK}>(base)</span>
            </Hint>
          )}
          {row.ref ? <RefTag reason={row.ref.reason} /> : row.summary && <span className="truncate text-text-tertiary">— {row.summary}</span>}
          <span className="ml-auto pl-3 text-text-tertiary">:{row.line}</span>
        </>
      );
    case "frame": {
      const marker = exitMarker(row);
      return (
        <span className={cn("flex min-w-0 items-center gap-1.5", BRANCH_TEXT)}>
          <span className="truncate">{row.text}</span>
          {marker && (
            <Hint text={marker.hint}>
              <ExitChip marker={marker} />
            </Hint>
          )}
        </span>
      );
    }
    case "fold":
      return (
        <Hint text={foldHint(row.count, row.counts, row.open)}>
          <span className={FOLD_CHIP}>
            {row.count} {row.count === 1 ? "call" : "calls"} · {row.reasons}
          </span>
        </Hint>
      );
    case "more":
      return <span className="text-text-tertiary">… more steps not shown</span>;
    case "call": {
      // A call into the project, a method with no body included, is what the flow is made of.
      const project = !!row.targetId;
      const impls = row.step.impls?.length ?? 0;
      return (
        <>
          <MaybeHint text={row.hidden ? hiddenHint(row.step.reason) : shownHint(row.step.reason)}>
            <span className={cn("truncate", project ? "font-semibold text-text-primary" : "text-text-secondary", row.hidden && "opacity-60")}>
              {row.title}
            </span>
          </MaybeHint>
          {impls > 0 && (
            <Hint text={implsHint(impls)}>
              <span className={QUIET_MARK}>{implementationsLabel(impls)}</span>
            </Hint>
          )}
          {row.step.effect && (
            <Hint text={effectHint(row.step.effect)}>
              <span className={EFFECT_CHIP}>{effectName(row.step.effect)}</span>
            </Hint>
          )}
          {row.hidden && <span className={QUIET_MARK}>{row.step.reason}</span>}
          {row.ref && <RefTag reason={row.ref.reason} />}
          <span className="ml-auto pl-3 text-text-tertiary">:{row.step.line}</span>
        </>
      );
    }
  }
}

function RefTag({ reason }: { reason: "shown-above" | "recursion" }) {
  return (
    <Hint text={reason === "recursion" ? RECURSION_HINT : SHOWN_ABOVE_HINT}>
      <span className={QUIET_MARK}>{reason === "recursion" ? "↻ recursion" : "shown above ↑"}</span>
    </Hint>
  );
}
