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
  nodeType?: keyof typeof nodeTypes;
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
};

export function FlowCanvas(props: FlowCanvasProps) {
  return (
    <ReactFlowProvider>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}

function Canvas({ rootId, nodes, edges, onSelect, inspector, nodeType = "apiNode", buildData, edgeLabel, selectedNodeId }: FlowCanvasProps) {
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
    void fitView({ padding: 0.2, duration: 200 });
  }, [nodesInitialized, placed, ranks, getNodes, setNodes, fitView]);

  return (
    <div className={placed ? "size-full" : "size-full opacity-0"}>
      <ReactFlow
        defaultNodes={initialNodes}
        defaultEdges={flowEdges}
        nodeTypes={nodeTypes}
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
        <Panel position="bottom-left">
          <LayerLegend />
        </Panel>
        <Panel position="top-right">
          <CanvasToolbar
            onSearch={() => {}}
            onFit={() => void fitView({ padding: 0.2, duration: 200 })}
            onExpand={() => {}}
            fitActive
          />
        </Panel>
        {inspector}
      </ReactFlow>
    </div>
  );
}
