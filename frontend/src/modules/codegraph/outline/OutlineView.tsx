import { useEffect, useMemo, useState, type KeyboardEvent } from "react";
import { ChevronDown, ChevronRight, CornerDownLeft, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
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
import { loadSteps, setCallVisibility, toggleOutlineRow } from "../flowActions";
import { effectName } from "../graph/graphView";
import { buildOutline, type OutlineRow } from "./buildOutline";

const ROW_HEIGHT = 26;
const INDENT = 16;

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

/** The node a row is about: the method it names, or the method it calls (else the one it is in). */
function nodeOf(row: OutlineRow): string | null {
  if (row.kind === "method") return row.nodeId;
  if (row.kind === "call") return row.targetId ?? row.ownerId;
  return null;
}

/**
 * The Outline mode of a flow tab: the call flow read top to bottom, as the
 * code runs it. Rows come from buildOutline; this draws them, windows them,
 * and turns clicks, keys and the context menu into store actions.
 */
export function OutlineView({ folderId, root, flow, nodes, selectedNodeId, onInspect, onShowInGraph, onOpenAt }: OutlineViewProps) {
  const byId = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);
  const rows = useMemo(
    () => (flow.steps ? buildOutline({ rootId: root, nodes: byId, steps: flow.steps, toggled: flow.toggled }) : []),
    [flow.steps, flow.toggled, root, byId],
  );
  const { scrollRef, indices, padTop, padBottom } = useVirtualRows(rows.length, ROW_HEIGHT);
  const [cursorAt, setCursor] = useState(0);
  // Closing a row can leave the cursor past the end.
  const cursor = Math.min(cursorAt, Math.max(0, rows.length - 1));
  const hasRows = rows.length > 0;

  // The selection from the Graph (or the inspector) brings its first row into
  // view — also once the steps arrive, if they were not there yet. Not on
  // every change of rows: opening a row must not jump the view back.
  useEffect(() => {
    if (!selectedNodeId) return;
    const index = rows.findIndex((r) => nodeOf(r) === selectedNodeId);
    if (index < 0) return;
    setCursor(index);
    scrollToIndex(index);
  }, [selectedNodeId, hasRows]);

  function scrollToIndex(index: number) {
    const el = scrollRef.current;
    if (!el) return;
    const top = index * ROW_HEIGHT;
    if (top < el.scrollTop || top + ROW_HEIGHT > el.scrollTop + el.clientHeight) el.scrollTop = Math.max(0, top - el.clientHeight / 3);
  }

  function toggle(row: OutlineRow, open: boolean) {
    if (row.kind === "fold" || ((row.kind === "call" || row.kind === "method") && row.expandable)) {
      toggleOutlineRow(folderId, root, row.key, open);
    }
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
        scrollToIndex(target);
      }
      return;
    }
    const id = nodeOf(row);
    if (id) onInspect(id);
  }

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const row = rows[cursor];
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const next = Math.max(0, Math.min(rows.length - 1, cursor + (e.key === "ArrowDown" ? 1 : -1)));
      setCursor(next);
      scrollToIndex(next);
    } else if (e.key === "ArrowRight" && row) {
      e.preventDefault();
      toggle(row, true);
    } else if (e.key === "ArrowLeft" && row) {
      e.preventDefault();
      toggle(row, false);
    } else if (e.key === "Enter") {
      e.preventDefault();
      activate(cursor);
    }
  };

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
  if (!flow.steps) return <p className="p-5 text-body-13 text-text-tertiary">Reading the flow…</p>;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {flow.steps.legacy && (
        <p className="border-b border-border-subtle px-4 py-2 text-caption-12 text-text-tertiary">
          This index was written before branches were recorded. Re-index to see them.
        </p>
      )}
      <div
        ref={scrollRef}
        data-scroll-box
        tabIndex={0}
        role="tree"
        aria-label="Flow outline"
        onKeyDown={onKeyDown}
        className="group/outline min-h-0 flex-1 overflow-auto py-2 text-code-12 outline-none"
      >
        <div style={{ height: padTop }} />
        {indices.map((index) => (
          <OutlineLine
            key={rows[index].key}
            row={rows[index]}
            selected={nodeOf(rows[index]) !== null && nodeOf(rows[index]) === selectedNodeId}
            focused={index === cursor}
            onActivate={() => activate(index)}
            onToggle={(open) => toggle(rows[index], open)}
            onShowInGraph={onShowInGraph}
            onOpenAt={onOpenAt}
          />
        ))}
        <div style={{ height: padBottom }} />
      </div>
    </div>
  );
}

function OutlineLine({
  row,
  selected,
  focused,
  onActivate,
  onToggle,
  onShowInGraph,
  onOpenAt,
}: {
  row: OutlineRow;
  selected: boolean;
  focused: boolean;
  onActivate: () => void;
  onToggle: (open: boolean) => void;
  onShowInGraph: (nodeId: string) => void;
  onOpenAt: (nodeId: string, line: number) => void;
}) {
  const expandable = (row.kind === "call" || row.kind === "method") && row.expandable;
  const open = (row.kind === "call" || row.kind === "method") && row.open;
  const line = (
    <div
      role="treeitem"
      aria-level={row.depth + 1}
      aria-selected={selected}
      aria-expanded={expandable ? open : row.kind === "fold" ? row.open : undefined}
      onClick={onActivate}
      style={{ height: ROW_HEIGHT, paddingLeft: 12 + row.depth * INDENT }}
      className={cn(
        "flex cursor-default items-center gap-1.5 pr-4 whitespace-nowrap",
        selected ? "bg-surface-raised" : "hover:bg-state-soft",
        // The keyboard cursor, drawn only while the tree has keyboard focus.
        focused && "group-focus-visible/outline:outline-1 group-focus-visible/outline:-outline-offset-1 group-focus-visible/outline:outline-border-focus/50",
      )}
    >
      <span
        className="flex size-4 shrink-0 items-center justify-center text-icon-secondary"
        onClick={(e) => {
          if (!expandable) return;
          e.stopPropagation();
          onToggle(!open);
        }}
      >
        {expandable ? open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" /> : null}
      </span>
      <RowBody row={row} />
    </div>
  );

  if (row.kind !== "call" && row.kind !== "method") return line;
  const nodeId = nodeOf(row)!;
  // Callee keys split at their first "#" into type and member, as Go reads them for "<Type>#*".
  const key = row.kind === "call" ? row.step.calleeKey : "";
  const hash = key.indexOf("#");
  const type = hash > 0 ? key.slice(0, hash) : "";
  const member = hash > 0 ? key.slice(hash + 1) : "";
  const at = row.kind === "call" ? row.step.line : row.line;
  const ownerId = row.kind === "call" ? row.ownerId : row.nodeId;
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{line}</ContextMenuTrigger>
      <ContextMenuContent>
        {type && member && (
          <>
            <ContextMenuLabel className="max-w-80 truncate font-mono">{key}</ContextMenuLabel>
            <ContextMenuItem onSelect={() => void setCallVisibility(key, "show")}>Always show {simpleName(type)}.{member}</ContextMenuItem>
            <ContextMenuItem onSelect={() => void setCallVisibility(key, "hide")}>Always hide {simpleName(type)}.{member}</ContextMenuItem>
            <ContextMenuItem onSelect={() => void setCallVisibility(`${type}#*`, "show")}>Always show every call on {simpleName(type)}</ContextMenuItem>
            <ContextMenuItem onSelect={() => void setCallVisibility(`${type}#*`, "hide")}>Always hide every call on {simpleName(type)}</ContextMenuItem>
            <ContextMenuItem
              onSelect={() => {
                void setCallVisibility(key, "");
                void setCallVisibility(`${type}#*`, "");
              }}
            >
              Reset to default
            </ContextMenuItem>
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
            <span title="The overridden method's own body" className="text-caption-11 text-text-tertiary">
              (base)
            </span>
          )}
          {row.ref ? <RefTag reason={row.ref.reason} /> : row.summary && <span className="truncate text-text-tertiary">— {row.summary}</span>}
          <span className="ml-auto pl-3 text-text-tertiary">:{row.line}</span>
        </>
      );
    case "frame":
      return (
        <span className="flex min-w-0 items-center gap-1 text-(--hue-blue)">
          <span className="truncate">{row.text}</span>
          {row.exits && <CornerDownLeft aria-label="leaves" className="size-3 shrink-0 text-text-tertiary" />}
        </span>
      );
    case "fold":
      return (
        <span className="truncate rounded-sm border border-dashed border-border px-1.5 text-caption-11 text-text-tertiary" title={row.reasons}>
          {row.open ? "−" : "+"}
          {row.count} {row.count === 1 ? "call" : "calls"} · {row.reasons}
        </span>
      );
    case "more":
      return <span className="text-text-tertiary">… more steps not shown</span>;
    case "call": {
      // A call into the project, a method with no body included, is what the flow is made of.
      const project = !!row.targetId;
      return (
        <>
          <span
            title={row.hidden ? `Hidden: ${row.step.reason}` : row.step.reason}
            className={cn("truncate", project ? "font-semibold text-text-primary" : "text-text-secondary", row.hidden && "opacity-60")}
          >
            {row.title}
          </span>
          {row.step.impls?.length ? (
            <span className="shrink-0 text-text-tertiary">
              ↳ {row.step.impls.length} {row.step.impls.length === 1 ? "implementation" : "implementations"}
            </span>
          ) : null}
          {row.step.effect && (
            <span className="shrink-0 rounded-sm border border-border-subtle px-1.5 text-caption-11 text-text-secondary">
              {effectName(row.step.effect)}
            </span>
          )}
          {row.hidden && <span className="shrink-0 text-caption-11 text-text-tertiary">{row.step.reason}</span>}
          {row.ref && <RefTag reason={row.ref.reason} />}
          <span className="ml-auto pl-3 text-text-tertiary">:{row.step.line}</span>
        </>
      );
    }
  }
}

function RefTag({ reason }: { reason: "shown-above" | "recursion" }) {
  return <span className="shrink-0 text-caption-11 text-text-tertiary">{reason === "recursion" ? "↻ recursion" : "shown above ↑"}</span>;
}
