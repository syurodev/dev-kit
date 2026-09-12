import { useEffect, useMemo } from "react";
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";
// Every consumer of this library imports its stylesheet — ERDView and the
// codegraph canvas each do. Without it here the plan modal is a heap of
// absolutely-unpositioned divs whenever the ERD has not been opened first,
// which is most of the time. Bundlers de-duplicate it.
import "@xyflow/react/dist/style.css";
import { Flame, TriangleAlert } from "lucide-react";
import { cn } from "@/lib/utils";
import type { GraphNode, GraphSpec } from "./graphSpec";
import { layoutTree } from "./treeLayout";

/**
 * The plan, drawn.
 *
 * The only file here that knows React Flow exists. What to draw is decided
 * in planGraph and where to put it in treeLayout, both pure and both tested;
 * this turns a GraphSpec into nodes and edges and gets out of the way. That
 * separation is not tidiness — a second producer for this canvas is planned
 * in Go, and it can only exist if the canvas takes data rather than a
 * library's types.
 */

const NODE_WIDTH = 208;
const NODE_HEIGHT = 74;

/**
 * When a row estimate is wrong enough to say so.
 *
 * Ten times out is the number every planner-tuning guide reaches for, and it
 * is roughly where a nested loop that should have been a hash join starts
 * costing more than the rest of the plan put together.
 */
const ROWS_OFF_LOUDLY = 10;

type PlanNodeData = { node: GraphNode; analyzed: boolean };

function PlanCard({ data }: NodeProps<Node<PlanNodeData>>) {
  const { node, analyzed } = data;
  const metrics = node.metrics;
  const share = metrics?.shareOfTime ?? 0;
  const misestimated = (metrics?.rowsOffBy ?? 0) >= ROWS_OFF_LOUDLY;

  return (
    <div
      className={cn(
        "flex w-52 flex-col gap-1 rounded-md border bg-surface-raised px-3 py-2",
        node.hottest ? "border-status-danger" : "border-border-subtle",
      )}
      style={{ height: NODE_HEIGHT }}
    >
      <Handle type="target" position={Position.Top} className="!size-1 !border-0 !bg-border" />

      <div className="flex items-center gap-1.5">
        {node.hottest && <Flame className="size-3 shrink-0 text-status-danger" />}
        <span className="truncate text-code-12-medium text-text-primary" title={node.label}>
          {node.label}
        </span>
      </div>

      {node.detail ? (
        <span className="truncate text-code-11 text-text-tertiary" title={node.detail}>
          {node.detail}
        </span>
      ) : null}

      {/*
        The bar is the share of the plan's own time, which is the one number
        that ranks the nodes against each other. Not a colour scale: a
        gradient from green to red asks the reader to hold a key in their
        head, and a length is read without one.
      */}
      {analyzed && metrics ? (
        <div className="mt-auto flex items-center gap-2">
          <div className="h-1 flex-1 overflow-hidden rounded-full bg-surface-well">
            <div
              className={cn(
                "h-full rounded-full",
                node.hottest ? "bg-status-danger" : "bg-status-info",
              )}
              // Sub-pixel shares still get a hairline: a node that took 0.2%
              // of the time took some, and an empty track says it took none.
              style={{ width: `${Math.max(share * 100, share > 0 ? 2 : 0)}%` }}
            />
          </div>
          <span className="shrink-0 text-code-11 tabular-nums text-text-secondary">
            {formatMillis(metrics.selfMillis)}
          </span>
        </div>
      ) : null}

      {misestimated && metrics ? (
        <span
          className="flex items-center gap-1 text-code-11 text-status-warning"
          title={`Planned for ${format(metrics.estimatedRows)} rows, found ${format(metrics.actualRows)}`}
        >
          <TriangleAlert className="size-3 shrink-0" />
          {`rows off by ${Math.round(metrics.rowsOffBy ?? 0)}×`}
        </span>
      ) : null}

      <Handle type="source" position={Position.Bottom} className="!size-1 !border-0 !bg-border" />
    </div>
  );
}

const nodeTypes = { plan: PlanCard };

function formatMillis(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms >= 1) return `${Math.round(ms)}ms`;
  return `${ms.toFixed(2)}ms`;
}

function format(rows: number): string {
  return rows.toLocaleString();
}

/** fitView needs measured nodes, so it waits for the first ones to arrive. */
function FitOnGraph({ count }: { count: number }) {
  const { fitView } = useReactFlow();
  useEffect(() => {
    if (count === 0) return;
    void fitView({ duration: 200, maxZoom: 1 });
  }, [count, fitView]);
  return null;
}

export function PlanDiagram({ graph }: { graph: GraphSpec }) {
  const { nodes, edges } = useMemo(() => {
    const places = layoutTree(graph.nodes, {
      nodeWidth: NODE_WIDTH,
      nodeHeight: NODE_HEIGHT,
      columnGap: 28,
      rowGap: 36,
    });
    return {
      nodes: graph.nodes.map<Node<PlanNodeData>>((node) => ({
        id: node.id,
        type: "plan",
        position: places.get(node.id) ?? { x: 0, y: 0 },
        data: { node, analyzed: graph.analyzed },
      })),
      edges: graph.edges.map<Edge>((edge) => ({
        id: edge.id,
        source: edge.source,
        target: edge.target,
        type: "smoothstep",
      })),
    };
  }, [graph]);

  if (graph.nodes.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center text-body-12 text-text-tertiary">
        The engine returned no plan to draw.
      </div>
    );
  }

  return (
    <ReactFlowProvider>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        // Read, not edited: the nodes are an answer, and dragging one would
        // suggest the arrangement means something the reader chose.
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        proOptions={{ hideAttribution: true }}
        minZoom={0.2}
      >
        <Background variant={BackgroundVariant.Dots} gap={16} size={1} className="opacity-40" />
        {/* The same treatment ERDView gives them: React Flow ships a light
            control bar, and unstyled it is three white boxes on a dark
            canvas. */}
        <Controls
          showInteractive={false}
          className="border-border bg-surface-raised fill-current text-text-primary"
        />
        <FitOnGraph count={nodes.length} />
      </ReactFlow>
    </ReactFlowProvider>
  );
}
