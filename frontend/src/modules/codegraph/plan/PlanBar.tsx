import { Button } from "@/components/ui/button";
import type { CodeGraphPlanProgress } from "@/bridge/codegraphPlan";

/**
 * Figma plan `canvas-header` left half (482:8255): the PLAN chip and the
 * plan's name, drawn into FlowCanvas's header slot.
 */
export function PlanHeader({ plan }: { plan: CodeGraphPlanProgress }) {
  return (
    <>
      <span className="rounded-sm bg-status-success px-2 py-0.75 text-code-12-medium text-action-primary-foreground">
        PLAN
      </span>
      <span className="truncate text-title-16 text-text-primary" title={plan.description}>
        {plan.description || "Untitled plan"}
      </span>
    </>
  );
}

/**
 * The right half: "in_progress · 2/5 · 5 nodes · 4 edges" and the one
 * human-only action.
 *
 * Approval is deliberately a single decision for the entire plan, not per node
 * — codegraph.mdx § "Bridge & MCP mới". A draft plan is inert until it happens.
 */
export function PlanMeta({
  plan,
  edgeCount,
  busy,
  onApprove,
  onRefresh,
}: {
  plan: CodeGraphPlanProgress;
  edgeCount: number;
  busy: boolean;
  onApprove: () => void;
  onRefresh: () => void;
}) {
  const done = plan.nodes.filter((n) => n.status === "implemented_matching").length;

  return (
    <>
      <span className="text-code-11 text-text-tertiary">
        {plan.aggregate} · {done}/{plan.nodes.length} · {plan.nodes.length} nodes · {edgeCount}{" "}
        edges
      </span>
      {plan.state === "draft" ? (
        <Button size="xs" disabled={busy} onClick={onApprove}>
          Approve plan
        </Button>
      ) : (
        <span className="text-code-11 text-text-tertiary">approved</span>
      )}
      <Button size="xs" variant="outline" className="bg-surface-raised" disabled={busy} onClick={onRefresh}>
        Refresh
      </Button>
    </>
  );
}
