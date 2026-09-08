import { Handle, Position, type NodeProps } from "@xyflow/react";
import { LAYERS } from "../graph/tokens";
import { inferPlanLayer } from "./planLayer";
import { PLAN_STATUS } from "./planStatus";
import { cn } from "@/lib/utils";
import type { PlanNodeStatus } from "@/bridge/codegraphPlan";

export type PlanNodeData = {
  title: string;
  fqn: string;
  status: PlanNodeStatus;
  commentCount: number;
};

/**
 * A node on the plan canvas. Same geometry as ApiNode so the two canvases read
 * as one language — the difference is that the border and badge carry status,
 * and the layer is inferred from the intended name rather than read from an
 * annotation that does not exist yet.
 */
export function PlanNode({ data, selected }: NodeProps & { data: PlanNodeData }) {
  const visual = LAYERS[inferPlanLayer(data.fqn)];
  const Icon = visual.icon;
  const accent = `var(${visual.cssVar})`;
  const status = PLAN_STATUS[data.status];

  return (
    <div
      title={status.hint}
      className={cn(
        "group flex w-67 flex-col gap-2.5 rounded-xl border bg-surface-raised p-3",
        status.border,
        selected && "shadow-[0_6px_16px_rgba(0,0,0,0.45)]",
      )}
    >
      <Handle id="top" type="target" position={Position.Top} className="!opacity-0" />
      <Handle id="left" type="target" position={Position.Left} className="!opacity-0" />
      <Handle id="bottom" type="source" position={Position.Bottom} className="!opacity-0" />

      <div className="flex items-center gap-2.5">
        <span
          className="flex size-7.5 shrink-0 items-center justify-center rounded-control"
          style={{ backgroundColor: `color-mix(in srgb, ${accent} 14%, transparent)` }}
        >
          <Icon size={16} strokeWidth={2} style={{ color: accent }} />
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span
            className="font-mono text-[9px] font-medium tracking-[0.06em]"
            style={{ color: accent }}
          >
            {visual.label}
          </span>
          <span title={data.fqn} className="truncate text-title-14 text-text-primary">
            {data.title}
          </span>
        </span>
        {data.commentCount > 0 && (
          <span
            title={`${data.commentCount} comment(s)`}
            className="shrink-0 rounded-full bg-foreground/10 px-1.5 text-caption-11 tabular-nums text-foreground/70"
          >
            {data.commentCount}
          </span>
        )}
      </div>

      <div className="flex items-center gap-2">
        <span className={cn("shrink-0 rounded px-1.5 py-0.5 text-caption-11-medium", status.badge)}>
          {status.label}
        </span>
        <span
          title={data.fqn}
          className="min-w-0 flex-1 truncate rounded-lg bg-surface-tinted px-2 py-1 text-right text-code-11 text-text-secondary"
        >
          {data.fqn}
        </span>
      </div>
    </div>
  );
}
