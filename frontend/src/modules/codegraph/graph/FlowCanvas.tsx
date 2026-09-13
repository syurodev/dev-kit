import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Background,
  MarkerType,
  Panel,
  ReactFlow,
  ReactFlowProvider,
  useNodesInitialized,
  useReactFlow,
  type Edge,
  type Node,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { ApiNode } from "./ApiNode";
import { PlanNode } from "../plan/PlanNode";
import { ConnectorEdge } from "./ConnectorEdge";
import { LayerLegend } from "./LayerLegend";
import { CanvasToolbar } from "./CanvasToolbar";
import { rankGraph } from "./rank";
import { place } from "./place";
import { shortLabel } from "./labels";
import type { CodeGraphEdge, CodeGraphNode } from "@/bridge/codegraph";

const nodeTypes = { apiNode: ApiNode, planNode: PlanNode };
const edgeTypes = { connector: ConnectorEdge };

/**
 * What fitView has to keep clear, and why it is measured in pixels.
 *
 * Two glass cards float over the viewport along its bottom edge — the layer
 * legend at bottom-left, the hint at bottom-right — and fitView knows nothing
 * about either. It centres the graph in the whole viewport, so with the
 * inspector open (which puts the canvas under 600px wide) the last node in a
 * vertical chain parks under the legend and has its file chip clipped by it.
 *
 * A numeric `padding` cannot fix it at any value: it is a fraction of the
 * GRAPH's bounds, not the viewport's, so the taller the graph the smaller the
 * real gap — the case needing the room is the one that loses it. Pixels are the
 * same on every graph.
 *
 * The bottom figure is the legend's own height plus React Flow's 15px panel
 * margin, with a little clearance. It is the taller of the two corners, and
 * the plan canvas stacks a second card on top of it.
 */
function fitPadding(planLegend: boolean | undefined, legend: boolean) {
  // `as const` so the values keep their literal types: React Flow's Padding is
  // a template literal (`${number}px`), and a widened `string` does not match.
  return {
    top: "24px",
    left: "24px",
    right: "24px",
    bottom: !legend ? "24px" : planLegend ? "184px" : "136px",
  } as const;
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
  /** Builds the node's `data`. Defaults to the API-node shape. */
  buildData?: (node: CodeGraphNode) => Record<string, unknown>;
  /** Per-edge label text, for plan conditions. */
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
  /** Right of the header, before the toolbar: "4 nodes · 3 edges" and friends. */
  meta?: ReactNode;
  /** `canvas-hint` (482:4564): the one-line glass card in the bottom-right. */
  hint?: ReactNode;
  /** Adds the dashed-border legend the plan canvas needs. */
  planLegend?: boolean;
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
}: FlowCanvasProps) {
  const { getNodes, setNodes, fitView } = useReactFlow();
  const nodesInitialized = useNodesInitialized();
  const [placed, setPlaced] = useState(false);

  const ranks = useMemo(
    () => rankGraph({ rootId, nodeIds: nodes.map((n) => n.id), edges }),
    [rootId, nodes, edges],
  );

  // Keep only reachable downstream nodes
  const reachableNodes = useMemo(
    () => nodes.filter((n) => ranks.has(n.id)),
    [nodes, ranks],
  );

  // Everything starts at the origin and stays invisible until it has been
  // measured and placed — that is what lets place() refuse to guess a size.
  const initialNodes: Node[] = useMemo(
    () =>
      reachableNodes.map((n) => ({
        id: n.id,
        type: nodeType,
        selected: n.id === selectedNodeId,
        position: { x: 0, y: 0 },
        data: buildData?.(n) ?? {
          layer: n.layer,
          // Short name on the card, fully-qualified name in the tooltip — the
          // card is too narrow to show an FQN without truncating away the only
          // part that identifies the node. See graph/labels.ts.
          title: shortLabel(n.label, n.kind),
          titleTooltip: n.label,
          detail: `${n.filePath.split("/").pop()}:${n.line}`,
          detailTooltip: `${n.filePath}:${n.line}`,
        },
      })),
    [reachableNodes, nodeType, buildData, selectedNodeId],
  );

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
              label: edgeLabel?.(e),
              showLabel: e.kind === "di" || !!edgeLabel?.(e),
            },
            markerEnd: { type: MarkerType.Arrow, color: "var(--icon-secondary)" },
          },
        ];
      }),
    [edges, ranks, edgeLabel],
  );

  useEffect(() => setPlaced(false), [initialNodes, flowEdges]);

  const fitOptions = useMemo(
    () => ({ padding: fitPadding(planLegend, legend), duration: 200 }),
    [planLegend, legend],
  );

  useEffect(() => {
    if (!nodesInitialized || placed) return;
    const sizes = new Map(
      getNodes().flatMap((n) =>
        n.measured?.width && n.measured?.height
          ? [[n.id, { width: n.measured.width, height: n.measured.height }] as const]
          : [],
      ),
    );
    const positions = place(ranks, sizes);
    if (!positions) return;
    setNodes((current) =>
      current.map((n) => ({ ...n, position: positions.get(n.id) ?? n.position })),
    );
    setPlaced(true);
    void fitView(fitOptions);
  }, [nodesInitialized, placed, ranks, getNodes, setNodes, fitView, fitOptions]);

  return (
    <div className="flex size-full flex-col px-6 pt-5">
      {/* Header lives outside ReactFlow but inside its provider, so the
          toolbar's fitView works and the row never pans with the graph. */}
      <div className="flex shrink-0 items-center justify-between gap-4 pb-1">
        <div className="flex min-w-0 items-center gap-2.5">{header}</div>
        <div className="flex shrink-0 items-center gap-3.5">
          {meta}
          <CanvasToolbar
            onSearch={() => {}}
            onFit={() => void fitView(fitOptions)}
            onExpand={() => {}}
            fitActive
          />
        </div>
      </div>
      <div className="h-px shrink-0 bg-border-subtle" />

      <div className={placed ? "min-h-0 flex-1" : "min-h-0 flex-1 opacity-0"}>
        <ReactFlow
          defaultNodes={initialNodes}
          defaultEdges={flowEdges}
          nodeTypes={types}
          edgeTypes={edgeTypes}
          // Read-only view: positions come from the layout, and nobody authors
          // edges by hand, so both editing affordances are off.
          nodesDraggable={false}
          nodesConnectable={false}
          onlyRenderVisibleElements
          onNodeClick={(_, node) => onSelect?.(node.id)}
          proOptions={{ hideAttribution: false }}
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
            <Panel position="bottom-right" style={{ marginBottom: 42 }}>
              <div className="rounded-md border border-glass-border bg-surface-glass-floating px-3 py-2.25 text-label-12 text-text-secondary shadow-[0_6px_10px_rgba(0,0,0,0.35)] backdrop-blur-lg">
                {hint}
              </div>
            </Panel>
          )}
          {inspector}
        </ReactFlow>
      </div>
    </div>
  );
}
