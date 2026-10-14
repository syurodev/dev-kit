import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import {
  applyNodeChanges,
  Background,
  MarkerType,
  Panel,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Edge,
  type Node,
  type NodeChange,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { ApiNode } from "./ApiNode";
import { PlanNode } from "../plan/PlanNode";
import { ConnectorEdge } from "./ConnectorEdge";
import { LayerLegend } from "./LayerLegend";
import { CanvasFitButton, CanvasToolbar } from "./CanvasToolbar";
import { rankGraph } from "./rank";
import { graphBounds, place, type Size } from "./place";
import { shortLabel } from "./labels";
import { flowThrough } from "./focus";
import type { CodeGraphEdge, CodeGraphNode } from "@/bridge/codegraph";

const nodeTypes = { apiNode: ApiNode, planNode: PlanNode };
const edgeTypes = { connector: ConnectorEdge };

/**
 * What fitView has to keep clear, and why it is measured in pixels.
 *
 * Two small glass controls sit over the viewport along its bottom edge — the
 * legend toggle at bottom-left, the hint at bottom-right — and fitView knows
 * nothing about either. It centres the graph in the whole viewport, so with
 * the inspector open (which puts the canvas under 600px wide) the last node
 * in a vertical chain parks under one and has its file chip clipped by it.
 *
 * A numeric `padding` cannot fix it at any value: it is a fraction of the
 * GRAPH's bounds, not the viewport's, so the taller the graph the smaller the
 * real gap — the case needing the room is the one that loses it. Pixels are the
 * same on every graph.
 *
 * The bottom figure is sized for the legend's collapsed trigger (it opens as
 * a popover on demand, so it no longer needs a card's worth of permanent
 * room — see LayerLegend) plus React Flow's 15px panel margin and a little
 * clearance. A generous reservation here was the biggest single reason a
 * tall, narrow plan graph fit with dead space down both sides: fitView keeps
 * the whole graph's aspect ratio, so shrinking the tighter (bottom) constraint
 * lets it zoom in and use the width too.
 */
function fitPadding(legend: boolean, compact: boolean) {
  // `as const` so the values keep their literal types: React Flow's Padding is
  // a template literal (`${number}px`), and a widened `string` does not match.
  if (compact) return { top: "12px", left: "12px", right: "12px", bottom: "12px" } as const;
  return {
    top: "16px",
    left: "16px",
    right: "16px",
    bottom: legend ? "56px" : "16px",
  } as const;
}

export type CanvasCounts = {
  /** Nodes drawn: reachable from the root. */
  shown: number;
  /** Nodes given but not drawn, because the root cannot reach them. */
  hidden: number;
  edges: number;
};

/** "12 nodes · 14 edges", plus the unreachable ones the canvas left out. */
export function describeCounts({ shown, hidden, edges }: CanvasCounts): string {
  const base = `${shown} nodes · ${edges} edges`;
  return hidden > 0 ? `${base} (${hidden} unreachable hidden)` : base;
}

const HIDDEN_STYLE = { opacity: 0 };

// Every card and connector carries the transition, so a card fades back as
// well as out: the class that dims is the only one that comes and goes.
const NODE_FADE = "transition-opacity duration-150";
const NODE_DIM = `${NODE_FADE} opacity-25`;
const EDGE_FADE = "transition-opacity duration-150";
const EDGE_DIM = `${EDGE_FADE} opacity-15`;

/**
 * One object per item and state, reused: React Flow re-renders a card whose
 * object changed, so rebuilding every node on each hover would redraw the
 * whole graph to dim part of it. Keyed weakly by the source object, so a
 * layout or data change (new objects) simply misses.
 */
function variant<T extends object>(cache: WeakMap<T, Map<string, T>>, source: T, state: string, make: () => T): T {
  let byState = cache.get(source);
  if (!byState) cache.set(source, (byState = new Map()));
  let made = byState.get(state);
  if (!made) byState.set(state, (made = make()));
  return made;
}

/** Card ids under the pointer or keyboard focus, from a React Flow node wrapper. */
function nodeIdOf(target: EventTarget | null): string | null {
  const el = (target as HTMLElement | null)?.closest?.(".react-flow__node");
  return el?.getAttribute("data-id") ?? null;
}

export type FlowCanvasProps = {
  rootId: string;
  nodes: CodeGraphNode[];
  edges: CodeGraphEdge[];
  onSelect?: (nodeId: string) => void;
  /** Rendered inside ReactFlow — NodeToolbar-based overlays need that context. */
  inspector?: ReactNode;
  /**
   * Which registered node component to render. The plan canvas reuses this
   * whole file — ranking, placement, connectors — and differs only in the card
   * it draws and the data that card needs.
   */
  nodeType?: string;
  /**
   * Card components for a canvas drawn outside codegraph — the database
   * module's query plan uses this whole file with its own card. Pass a
   * module-level constant: React Flow remounts every node on a new object.
   */
  nodeTypes?: NodeTypes;
  /** The layer legend. Off where the cards are not controllers and services. */
  legend?: boolean;
  /**
   * Builds the node's `data`. Defaults to the API-node shape. An `ariaLabel`
   * string in the result names the card for a screen reader. Pass a stable
   * function (module-level or useCallback): a new one rebuilds every card.
   */
  buildData?: (node: CodeGraphNode) => Record<string, unknown>;
  /** Per-edge label text, for plan conditions. Stable, like buildData. */
  edgeLabel?: (edge: CodeGraphEdge) => string | undefined;
  /**
   * Marks one node selected from outside the canvas — picking a row in the
   * sidebar should show you which card it means, and React Flow's own
   * selection only reacts to clicks on the canvas itself.
   */
  selectedNodeId?: string | null;
  /**
   * Figma `canvas-header` (482:4528): what the graph is, at the left — a
   * method chip and a path, or the PLAN chip and its name.
   */
  header?: ReactNode;
  /**
   * Right of the header, before the toolbar: "4 nodes · 3 edges" and friends.
   * As a function it gets what the canvas actually draws — nodes the root
   * cannot reach are dropped, so the caller's own count would overstate it.
   */
  meta?: ReactNode | ((counts: CanvasCounts) => ReactNode);
  /** `canvas-hint` (482:4564): the one-line glass card in the bottom-right. */
  hint?: ReactNode;
  /** Adds the dashed-border legend the plan canvas needs. */
  planLegend?: boolean;
  /**
   * A glance in a card rather than a document: no header strip (its search
   * and expand do nothing there), one fit button over the canvas, never
   * zoomed past 100% to fill the box, and the wheel scrolls the page around
   * it rather than zooming a canvas the reader was scrolling past.
   */
  compact?: boolean;
  /**
   * React Flow's "React Flow" badge. On by default, as codegraph keeps it; a
   * canvas whose box is too small for it to read (or whose dark ground it
   * cannot read on) turns it off.
   */
  attribution?: boolean;
  /** The placed graph's unscaled size, after every layout — for a box that sizes to it. */
  onGraphSize?: (size: Size) => void;
};

export function FlowCanvas(props: FlowCanvasProps) {
  return (
    <ReactFlowProvider>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}

function Canvas({
  rootId,
  nodes,
  edges,
  onSelect,
  inspector,
  nodeType = "apiNode",
  nodeTypes: types = nodeTypes,
  legend = true,
  buildData,
  edgeLabel,
  selectedNodeId,
  header,
  meta,
  hint,
  planLegend,
  compact = false,
  attribution = true,
  onGraphSize,
}: FlowCanvasProps) {
  const { fitView } = useReactFlow();

  const ranks = useMemo(
    () => rankGraph({ rootId, nodeIds: nodes.map((n) => n.id), edges }),
    [rootId, nodes, edges],
  );

  // Keep only reachable downstream nodes
  const reachableNodes = useMemo(
    () => nodes.filter((n) => ranks.has(n.id)),
    [nodes, ranks],
  );

  // What each card should say. Positions are NOT here: they belong to the
  // layout below, and a data-only change (a plan node's status, a comment
  // count) must reach the card without moving it.
  const desiredNodes = useMemo(
    () =>
      reachableNodes.map((n) => {
        const data = buildData?.(n) ?? {
          layer: n.layer,
          // Short name on the card, fully-qualified name in the tooltip — the
          // card is too narrow to show an FQN without truncating away the only
          // part that identifies the node. See graph/labels.ts.
          title: shortLabel(n.label, n.kind),
          titleTooltip: n.label,
          detail: `${n.filePath.split("/").pop()}:${n.line}`,
          detailTooltip: `${n.filePath}:${n.line}`,
          ariaLabel: `${n.layer || n.kind} ${n.label}`,
        };
        return {
          id: n.id,
          type: nodeType,
          data,
          // React Flow's own node wrapper is the focusable element (it already
          // has tabIndex=0); making it a named button, rather than nesting a
          // second focusable inside the card, keeps one tab stop per node.
          ariaRole: "button" as const,
          ariaLabel: typeof data.ariaLabel === "string" ? data.ariaLabel : n.label,
        };
      }),
    [reachableNodes, nodeType, buildData],
  );

  // Controlled, not defaultNodes: React Flow reads defaultNodes once, so a
  // "Load more" page, a background re-index or a plan status change never
  // reached the canvas — and the new ids were never measured, so the canvas
  // stayed hidden for good. Every node starts at the origin until placed.
  const [rfNodes, setRfNodes] = useState<Node[]>(() =>
    desiredNodes.map((d) => ({ ...d, selected: d.id === selectedNodeId, position: { x: 0, y: 0 } })),
  );
  const onNodesChange = useCallback(
    (changes: NodeChange[]) => setRfNodes((current) => applyNodeChanges(changes, current)),
    [],
  );

  useEffect(() => {
    setRfNodes((previous) => {
      const byId = new Map(previous.map((n) => [n.id, n]));
      return desiredNodes.map((d) => {
        const old = byId.get(d.id);
        const selected =
          selectedNodeId === undefined ? (old?.selected ?? false) : d.id === selectedNodeId;
        // An existing node keeps its position and measurements; only what it
        // says changes.
        return old ? { ...old, ...d, selected } : { ...d, selected, position: { x: 0, y: 0 } };
      });
    });
  }, [desiredNodes, selectedNodeId]);

  const fitOptions = useMemo(
    () => ({ padding: fitPadding(legend, compact), duration: 200, ...(compact ? { maxZoom: 1 } : {}) }),
    [legend, compact],
  );
  const onGraphSizeRef = useRef(onGraphSize);
  onGraphSizeRef.current = onGraphSize;

  // The layout is a function of the ranking alone, so it re-runs when the
  // ranking does (a new node, a new edge) and never on a data-only update.
  const layoutKey = useMemo(
    () => [...ranks].map(([id, r]) => `${id}:${r.rank}:${r.order}`).join("|"),
    [ranks],
  );
  const [placedKey, setPlacedKey] = useState<string | null>(null);
  const [placedIds, setPlacedIds] = useState<ReadonlySet<string>>(() => new Set());
  const lastPlacedIds = useRef<ReadonlySet<string> | null>(null);
  const placing = placedKey !== layoutKey;

  useEffect(() => {
    if (!placing) return;
    const sizes = new Map(
      rfNodes.flatMap((n) =>
        n.measured?.width && n.measured?.height
          ? [[n.id, { width: n.measured.width, height: n.measured.height }] as const]
          : [],
      ),
    );
    // Null until every ranked node exists and has been measured.
    const positions = place(ranks, sizes);
    if (!positions) return;
    setRfNodes((current) =>
      current.map((n) => {
        const p = positions.get(n.id);
        return p && (p.x !== n.position.x || p.y !== n.position.y) ? { ...n, position: p } : n;
      }),
    );
    const ids = new Set(positions.keys());
    const previous = lastPlacedIds.current;
    const nodeSetChanged =
      !previous || previous.size !== ids.size || [...ids].some((id) => !previous.has(id));
    lastPlacedIds.current = ids;
    setPlacedIds(ids);
    setPlacedKey(layoutKey);
    const bounds = graphBounds(positions, sizes);
    if (bounds) onGraphSizeRef.current?.(bounds);
    // Fit on the first placement and when the node set changed; a re-layout
    // of the same nodes keeps whatever pan and zoom the user chose. fitView is
    // queued by React Flow until the new positions have reached its store.
    if (nodeSetChanged) void fitView(fitOptions);
  }, [placing, rfNodes, ranks, layoutKey, fitView, fitOptions]);

  // The view the layout fitted stays fitted while its pane changes size — a
  // group split beside it, the inspector opening, the window shrinking.
  // React Flow does not refit on resize, so without this a narrowed canvas
  // kept its old zoom and cut the graph off at the right edge. Once the user
  // pans or zooms, the view is theirs and a resize leaves it alone.
  const viewport = useRef<HTMLDivElement>(null);
  const userMoved = useRef(false);
  const fitRef = useRef(() => {});
  fitRef.current = () => {
    if (!userMoved.current && placedIds.size > 0) void fitView({ ...fitOptions, duration: 0 });
  };
  useEffect(() => {
    const el = viewport.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let frame = 0;
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => fitRef.current());
    });
    observer.observe(el);
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, []);


  const flowEdges: Edge[] = useMemo(
    () =>
      edges.flatMap((e) => {
        const from = ranks.get(e.from);
        const to = ranks.get(e.to);
        if (!from || !to) return [];
        // A target in the same column directly descending by 1 rank continues
        // the trunk and enters from the top; anything offset sideways or skipping
        // ranks is a branch and enters from the left to avoid crossing intermediate cards.
        const isDirectNextRank = to.rank - from.rank === 1;
        const sameColumn = from.order === to.order && isDirectNextRank;
        const label = edgeLabel?.(e);
        return [
          {
            id: `${e.from}->${e.to}`,
            source: e.from,
            target: e.to,
            sourceHandle: "bottom",
            targetHandle: sameColumn ? "top" : "left",
            type: "connector",
            data: {
              kind: e.kind,
              label,
              showLabel: e.kind === "di" || !!label,
            },
            markerEnd: { type: MarkerType.Arrow, color: "var(--icon-secondary)" },
          },
        ];
      }),
    [edges, ranks, edgeLabel],
  );
  // The card under the pointer (or focused from the keyboard), and the flow
  // through it: what leads there and what it leads to stay lit, the rest
  // dims, so one chain can be read in a graph of twenty. See focus.ts.
  const [focusedId, setFocusedId] = useState<string | null>(null);
  const focus = useMemo(
    () => (focusedId && ranks.has(focusedId) ? flowThrough(focusedId, flowEdges) : null),
    [focusedId, ranks, flowEdges],
  );

  // A node not yet placed sits at the origin: drawn invisibly (so it can be
  // measured) and with no edges, instead of flashing over the graph.
  const nodeVariants = useRef(new WeakMap<Node, Map<string, Node>>());
  const shownNodes = useMemo(
    () =>
      rfNodes.map((n) => {
        if (!placedIds.has(n.id)) return { ...n, style: HIDDEN_STYLE };
        const className = focus && !focus.nodes.has(n.id) ? NODE_DIM : NODE_FADE;
        return variant(nodeVariants.current, n, className, () => ({ ...n, className }));
      }),
    [rfNodes, placedIds, focus],
  );

  const edgeVariants = useRef(new WeakMap<Edge, Map<string, Edge>>());
  const shownEdges = useMemo(
    () =>
      flowEdges
        .filter((e) => placedIds.has(e.source) && placedIds.has(e.target))
        .map((e) => {
          const state = focus === null ? "plain" : focus.edges.has(e.id) ? "lit" : "dimmed";
          return variant(edgeVariants.current, e, state, () => ({
            ...e,
            className: state === "dimmed" ? EDGE_DIM : EDGE_FADE,
            data: { ...e.data, dimmed: state === "dimmed", lit: state === "lit" },
            markerEnd: { type: MarkerType.Arrow, color: state === "lit" ? "var(--text-primary)" : "var(--icon-secondary)" },
          }));
        }),
    [flowEdges, placedIds, focus],
  );

  const counts: CanvasCounts = {
    shown: reachableNodes.length,
    hidden: nodes.length - reachableNodes.length,
    edges: flowEdges.length,
  };

  // Enter/Space on a focused card does what a click does. React Flow's own
  // key handler on the node only selects it, and node wrappers take no
  // onKeyDown of ours, so this listens where the event bubbles to.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "Enter" && e.key !== " ") return;
    const target = e.target as HTMLElement;
    if (!target.classList?.contains("react-flow__node")) return;
    const id = target.getAttribute("data-id");
    if (!id) return;
    e.preventDefault();
    onSelect?.(id);
  };

  return (
    <div className={compact ? "flex size-full flex-col" : "flex size-full flex-col px-4 pt-3"}>
      {/* Header lives outside ReactFlow but inside its provider, so the
          toolbar's fitView works and the row never pans with the graph. */}
      {compact ? null : (
        <div className="flex shrink-0 items-center justify-between gap-4 pb-1">
          <div className="flex min-w-0 items-center gap-2.5">{header}</div>
          <div className="flex shrink-0 items-center gap-3.5">
            {typeof meta === "function" ? meta(counts) : meta}
            <CanvasToolbar
              onFit={() => {
                // Asking for a fit hands the view back to the layout.
                userMoved.current = false;
                void fitView(fitOptions);
              }}
              fitActive
            />
          </div>
        </div>
      )}
      {compact ? null : <div className="h-px shrink-0 bg-border-subtle" />}

      <div
        ref={viewport}
        className={placedIds.size > 0 || !placing ? "min-h-0 flex-1" : "min-h-0 flex-1 opacity-0"}
        onKeyDown={onKeyDown}
        // Keyboard focus lights a flow the way hovering does. Captured here:
        // React Flow's node wrappers take no focus handlers of ours.
        onFocus={(e) => setFocusedId(nodeIdOf(e.target))}
        onBlur={(e) => {
          if (!nodeIdOf(e.relatedTarget)) setFocusedId(null);
        }}
      >
        <ReactFlow
          nodes={shownNodes}
          edges={shownEdges}
          onNodesChange={onNodesChange}
          nodeTypes={types}
          edgeTypes={edgeTypes}
          // Auto-layout places every node, but a reader still wants to drag
          // one clear of a crowded spot without losing that placement — the
          // sync effect above already keeps an existing node's position
          // untouched on a data-only refresh, so a drag survives until the
          // graph's own topology changes (a new node or edge) triggers a
          // fresh layout. Edges stay uneditable: nobody authors one by hand.
          nodesDraggable
          nodesConnectable={false}
          // Off while placing: a node outside the viewport is not rendered,
          // so it is never measured, and place() would wait for it forever.
          onlyRenderVisibleElements={!placing}
          onNodeClick={(_, node) => onSelect?.(node.id)}
          onNodeMouseEnter={(_, node) => setFocusedId(node.id)}
          onNodeMouseLeave={() => setFocusedId(null)}
          // A move with an event is the user's; fitView's own moves have none.
          onMoveStart={(event) => {
            if (event) userMoved.current = true;
          }}
          proOptions={{ hideAttribution: !attribution }}
          zoomOnScroll={!compact}
          preventScrolling={!compact}
        >
          <Background gap={24} size={2} color="color-mix(in srgb, var(--text-primary) 8%, transparent)" />
          {legend && (
            <Panel position="bottom-left">
              <LayerLegend plan={planLegend} />
            </Panel>
          )}
          {hint && (
            // Lifted clear of React Flow's attribution, which is itself a
            // bottom-right panel: at the default margin the two overlapped by
            // a few pixels and the badge read as a clipped grey artefact
            // behind the hint. The attribution stays — it is 19px tall, and
            // this is React Flow's 15px panel margin plus that plus a gap.
            <Panel position="bottom-right" style={{ marginBottom: 'var(--space-graph-bottom)' }}>
              <div className="rounded-md border border-glass-border bg-surface-glass-floating px-3 py-2.25 text-label-12 text-text-secondary shadow-canvas-overlay backdrop-blur-lg">
                {hint}
              </div>
            </Panel>
          )}
          {compact && (
            <Panel position="top-right">
              <CanvasFitButton
                onFit={() => {
                  userMoved.current = false;
                  void fitView(fitOptions);
                }}
              />
            </Panel>
          )}
          {inspector}
        </ReactFlow>
      </div>
    </div>
  );
}
