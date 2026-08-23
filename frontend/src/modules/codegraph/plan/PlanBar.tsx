import { Button } from "@/components/ui/button";
import { aggregateLabel } from "./planStatus";
import type { CodeGraphPlanProgress } from "@/bridge/codegraphPlan";

/**
 * Whole-plan state and the one human-only action.
 *
 * Approval is deliberately a single decision for the entire plan, not per node
 * — codegraph.mdx § "Bridge & MCP mới". A draft plan is inert until it happens.
 */
export function PlanBar({
  plan,
  busy,
  onApprove,
  onRefresh,
}: {
  plan: CodeGraphPlanProgress;
  busy: boolean;
  onApprove: () => void;
  onRefresh: () => void;
}) {
  const done = plan.nodes.filter((n) => n.status === "implemented_matching").length;

  return (
    <div className="flex items-center gap-2 border-b border-border-subtle px-3 py-1.5">
      <span className="text-xs font-medium text-text-primary">{aggregateLabel(plan.aggregate)}</span>
      <span className="font-mono text-[11px] text-text-tertiary">
        {done}/{plan.nodes.length} done
      </span>
      {plan.description && (
        <span className="min-w-0 flex-1 truncate text-[11px] text-text-tertiary" title={plan.description}>
          {plan.description}
        </span>
      )}
      {plan.state === "draft" ? (
        <Button
          size="sm"
          className="ml-auto h-6 shrink-0 px-2 text-xs"
          disabled={busy}
          onClick={onApprove}
        >
          Approve plan
        </Button>
      ) : (
        <span className="ml-auto shrink-0 text-[11px] text-text-tertiary">approved</span>
      )}
      <Button
        size="sm"
        variant="secondary"
        className="h-6 shrink-0 px-2 text-xs"
        disabled={busy}
        onClick={onRefresh}
      >
        Refresh
      </Button>
    </div>
  );
}
