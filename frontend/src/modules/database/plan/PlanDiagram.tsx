import { useCallback, useMemo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import {
  ArrowDownUp,
  Box,
  Flame,
  GitMerge,
  Sigma,
  Table2,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import { FlowCanvas } from "@/modules/codegraph/graph/FlowCanvas";
import { ICON_BADGE } from "@/modules/codegraph/graph/tokens";
import type { CodeGraphEdge, CodeGraphNode } from "@/bridge/codegraph";
import { cn } from "@/lib/utils";
import type { GraphNode, GraphSpec } from "./graphSpec";
import { planStepKind, type PlanStepKind } from "./planGraph";

/**
 * The plan, drawn on the code graph's canvas.
 *
 * FlowCanvas whole — its ranking, placement, elbow connectors, dot grid and
 * fit toolbar — so a query plan and a call graph read as one language. What
 * is ours is the card and the adapter from GraphSpec to the canvas's shape.
 */

/**
 * When a row estimate is wrong enough to say so.
 *
 * Ten times out is the number every planner-tuning guide reaches for, and it
 * is roughly where a nested loop that should have been a hash join starts
 * costing more than the rest of the plan put together.
 */
const ROWS_OFF_LOUDLY = 10;

/** The code graph's layer hues, by the same logic: storage is yellow, flow is blue. */
const STEPS: Record<PlanStepKind, { cssVar: string; icon: LucideIcon; label: string }> = {
  scan: { cssVar: "--hue-yellow", icon: Table2, label: "SCAN" },
  join: { cssVar: "--hue-blue", icon: GitMerge, label: "JOIN" },
  sort: { cssVar: "--hue-green", icon: ArrowDownUp, label: "SORT" },
  aggregate: { cssVar: "--hue-green", icon: Sigma, label: "AGGREGATE" },
  other: { cssVar: "--text-tertiary", icon: Box, label: "STEP" },
};

type PlanCardData = { node: GraphNode; analyzed: boolean };

/** ApiNode's geometry, with the plan's numbers in the detail well's place. */
function PlanCard({ data }: NodeProps & { data: PlanCardData }) {
  const { node, analyzed } = data;
  const step = STEPS[planStepKind(node.label)];
  const Icon = step.icon;
  const accent = `var(${step.cssVar})`;
  const metrics = node.metrics;
  const share = metrics?.shareOfTime ?? 0;
  const misestimated = (metrics?.rowsOffBy ?? 0) >= ROWS_OFF_LOUDLY;

  return (
    <div
      className={cn(
        "flex w-67 flex-col gap-2.5 rounded-lg border bg-surface-raised p-3",
        node.hottest ? "border-status-danger" : "border-border-subtle",
      )}
    >
      <Handle id="top" type="target" position={Position.Top} className="!opacity-0" />
      <Handle id="left" type="target" position={Position.Left} className="!opacity-0" />
      <Handle id="bottom" type="source" position={Position.Bottom} className="!opacity-0" />

      <div className="flex items-center gap-2.5">
        <span className={ICON_BADGE}>
          <Icon size={16} strokeWidth={2} style={{ color: accent }} />
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-label-11-caps" style={{ color: accent }}>
            {step.label}
          </span>
          <span title={node.label} className="truncate text-title-14 text-text-primary">
            {node.label}
          </span>
        </span>
        {node.hottest && <Flame className="size-4 shrink-0 text-status-danger" />}
      </div>

      {node.detail ? (
        <span
          title={node.detail}
          className="truncate rounded-md bg-state-soft px-2.5 py-1.75 text-code-11 text-text-secondary"
        >
          {node.detail}
        </span>
      ) : null}

      {/* The bar is the share of the plan's own time — a length is read
          without a key, a colour scale is not. */}
      {analyzed && metrics ? (
        <div className="flex items-center gap-2">
          <div className="h-1 flex-1 overflow-hidden rounded-full bg-state-soft">
            <div
              className={cn("h-full rounded-full", node.hottest ? "bg-status-danger" : "bg-(--hue-blue)")}
              // A node that took 0.2% of the time took some; an empty track says none.
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
          title={`Planned for ${metrics.estimatedRows.toLocaleString()} rows, found ${metrics.actualRows.toLocaleString()}`}
        >
          <TriangleAlert className="size-3 shrink-0" />
          {`rows off by ${Math.round(metrics.rowsOffBy ?? 0)}×`}
        </span>
      ) : null}
    </div>
  );
}

const nodeTypes = { queryPlan: PlanCard };

function formatMillis(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms >= 1) return `${Math.round(ms)}ms`;
  return `${ms.toFixed(2)}ms`;
}

/** The canvas ranks from one root; a plan with several tops gets one above them. */
const SHARED_ROOT = "plan-root";

function toCanvasNode(node: GraphNode): CodeGraphNode {
  return {
    id: node.id,
    label: node.label,
    kind: node.kind,
    layer: "",
    filePath: "",
    line: 0,
    endLine: 0,
    summary: node.detail,
  };
}

export function PlanDiagram({
  graph,
  compact = false,
  onGraphSize,
}: {
  graph: GraphSpec;
  /** The card's glance: see FlowCanvas's `compact`. The console's Explain keeps the full canvas. */
  compact?: boolean;
  onGraphSize?: (size: { width: number; height: number }) => void;
}) {
  const canvas = useMemo(() => {
    const byId = new Map(graph.nodes.map((node) => [node.id, node]));
    const nodes = graph.nodes.map(toCanvasNode);
    const edges: CodeGraphEdge[] = graph.edges.map((e) => ({ from: e.source, to: e.target, kind: "call" }));
    const roots = graph.nodes.filter((node) => node.parentId === null);
    if (roots.length <= 1) return { rootId: roots[0]?.id ?? "", nodes, edges, byId };

    const top: GraphNode = {
      id: SHARED_ROOT,
      label: "Query",
      detail: `${roots.length} top-level steps`,
      kind: "operation",
      depth: -1,
      parentId: null,
      metrics: null,
      hottest: false,
    };
    byId.set(SHARED_ROOT, top);
    return {
      rootId: SHARED_ROOT,
      nodes: [toCanvasNode(top), ...nodes],
      edges: [...roots.map((r) => ({ from: SHARED_ROOT, to: r.id, kind: "call" })), ...edges],
      byId,
    };
  }, [graph]);

  // Stable, because FlowCanvas memoises its nodes on it.
  const buildData = useCallback(
    (n: CodeGraphNode): PlanCardData => ({ node: canvas.byId.get(n.id)!, analyzed: graph.analyzed }),
    [canvas, graph.analyzed],
  );

  if (graph.nodes.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center text-body-12 text-text-tertiary">
        The engine returned no plan to draw.
      </div>
    );
  }

  return (
    <FlowCanvas
      rootId={canvas.rootId}
      nodes={canvas.nodes}
      edges={canvas.edges}
      nodeType="queryPlan"
      nodeTypes={nodeTypes}
      legend={false}
      buildData={buildData}
      compact={compact}
      onGraphSize={onGraphSize}
      // React Flow's badge sat bottom-right as a pale box nobody could read on
      // the dark ground, in the card and in the console's Explain alike.
      attribution={false}
    />
  );
}
