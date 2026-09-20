import { ListChecks, MessageSquare } from "lucide-react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { ICON_BADGE, LAYERS } from "../graph/tokens";
import { inferPlanLayer } from "./planLayer";
import { PLAN_BADGE, PLAN_STATUS } from "./planStatus";
import { cn } from "@/lib/utils";
import type { PlanNodeStatus } from "@/bridge/codegraphPlan";
import { dispatchIntent } from "@/workbench/intents";
import type { PlanTaskChip } from "../codegraphStore";

export type PlanNodeData = {
  title: string;
  fqn: string;
  status: PlanNodeStatus;
  commentCount: number;
  /**
   * The tasks implementing this node. Empty for most projects — a board is
   * optional — and the row is then not drawn rather than drawn empty.
   */
  tasks?: PlanTaskChip[];
};

/**
 * Figma `Plan Node` (232:268). Same geometry as ApiNode so the two canvases
 * read as one language — the difference is that the border and the chip in
 * the detail well carry status, a comment count sits on the top-right
 * corner, and the layer is inferred from the intended name rather than read
 * from an annotation that does not exist yet.
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
        "group relative flex w-67 flex-col gap-2.5 rounded-lg border bg-surface-raised p-3 in-focus-visible:ring-2 in-focus-visible:ring-text-primary",
        status.border,
        selected
          ? "shadow-[0_6px_8px_rgba(0,0,0,0.45)]"
          : "shadow-[0_1px_1.5px_rgba(0,0,0,0.18)]",
      )}
    >
      <Handle id="top" type="target" position={Position.Top} className="!opacity-0" />
      <Handle id="left" type="target" position={Position.Left} className="!opacity-0" />
      <Handle id="bottom" type="source" position={Position.Bottom} className="!opacity-0" />

      {data.commentCount > 0 && (
        <span
          title={`${data.commentCount} comment(s)`}
          className="absolute -top-2.25 right-3 flex h-4.5 items-center gap-1 rounded-full border border-border-subtle bg-state-soft pr-1.75 pl-1.5 text-code-11 text-text-secondary tabular-nums"
        >
          <MessageSquare size={11} />
          {data.commentCount}
        </span>
      )}

      <div className="flex items-center gap-2.5">
        <span className={ICON_BADGE}>
          <Icon size={16} strokeWidth={2} style={{ color: accent }} />
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-label-11-caps" style={{ color: accent }}>
            {visual.label}
          </span>
          <span title={data.fqn} className="truncate text-title-14 text-text-primary">
            {data.title}
          </span>
        </span>
      </div>

      {/* Tasks under the node, not on it: a plan node says what the code
          should be, a task says who is doing it and where that has got to.
          Clicking one opens it beside, so the plan you are reading stays. */}
      {data.tasks && data.tasks.length > 0 ? (
        <div className="flex flex-wrap gap-1">
          {data.tasks.map((task) => (
            <button
              key={task.id}
              type="button"
              title={`${task.key} · ${task.title} · ${task.status}`}
              aria-label={`Open task ${task.key}, ${task.title}`}
              onClick={(event) => {
                // The canvas selects a node on click; opening a task is not
                // also selecting the node it sits on.
                event.stopPropagation();
                void dispatchIntent("tasks.reveal", {
                  module: "tasks",
                  kind: "task",
                  id: task.id,
                  label: `${task.key} ${task.title}`,
                });
              }}
              className="flex h-4.5 max-w-full cursor-pointer items-center gap-1 rounded-full border border-border-subtle bg-state-soft pr-1.75 pl-1.5 text-code-11 text-text-secondary hover:text-text-primary"
            >
              <ListChecks size={11} className="shrink-0" />
              <span className="truncate">{task.key}</span>
            </button>
          ))}
        </div>
      ) : null}

      <div className="flex items-center gap-2 rounded-md bg-state-soft px-2.5 py-1.75">
        <span
          title={data.fqn}
          className="min-w-0 flex-1 truncate text-code-11 text-text-secondary"
        >
          {data.fqn}
        </span>
        <span className={cn(PLAN_BADGE, status.badge)}>{status.label}</span>
      </div>
    </div>
  );
}
