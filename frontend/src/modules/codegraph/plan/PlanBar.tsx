import { AnimatedNumber } from "@/components/ui/animated-number";
import { Check, Info, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { CodeGraphPlanProgress } from "@/bridge/codegraphPlan";
import { aggregateLabel, PLAN_BADGE } from "./planStatus";

/**
 * What the empty state (PlanDocument's "No plan for this project yet") tells
 * a project before a plan exists. Once one does, nothing repeated it — the
 * canvas dropped a first-time reader straight into nodes, edges and status
 * badges with no orientation to any of them.
 */
function PlanExplainer() {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="What is this?"
          className="flex size-5 shrink-0 items-center justify-center rounded-full text-text-tertiary hover:bg-state-soft hover:text-text-primary"
        >
          <Info size={14} />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80">
        <PopoverHeader>
          <PopoverTitle>What you're looking at</PopoverTitle>
          <PopoverDescription>
            An agent proposed this flow with codegraph.proposeFlow: the places in the
            code this change touches, and the order they need to happen in.
          </PopoverDescription>
        </PopoverHeader>
        <PopoverDescription>
          <strong className="text-text-primary">A node</strong> is one location — a
          class, method or field. Two nodes can share a short name; the smaller path
          text underneath (and the tooltip on hover) is what tells them apart.
        </PopoverDescription>
        <PopoverDescription>
          <strong className="text-text-primary">An edge's label</strong> is a
          constraint on that order (e.g. "the column must exist before the entity maps
          it"), not a function call — read the graph top to bottom as the intended
          sequence of work.
        </PopoverDescription>
        <PopoverDescription>
          <strong className="text-text-primary">The badge</strong> says how far the
          code has caught up to the plan for that node: Done matches it, Needs change
          doesn't yet, Planned hasn't been started.
        </PopoverDescription>
      </PopoverContent>
    </Popover>
  );
}

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
      <PlanExplainer />
    </>
  );
}

/**
 * The right half: "In progress · 2/5 nodes · 4 edges" and the one
 * human-only action.
 *
 * Approval is deliberately a single decision for the entire plan, not per node
 * — codegraph.mdx § "Bridge & MCP mới". A draft plan is inert until it happens.
 *
 * The count folds the done/total fraction and the node total into one
 * segment ("2/5 nodes" rather than "2/5 · 5 nodes", which said the same 5
 * twice) and Refresh drops its label — every other canvas control
 * (CanvasToolbar's search/fit/expand) is already icon-only with a tooltip,
 * and this is the one row that hadn't caught up.
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
      <span className="text-code-11 whitespace-nowrap text-text-tertiary">
        {aggregateLabel(plan.aggregate)} · <AnimatedNumber value={done} />/<AnimatedNumber value={plan.nodes.length} /> nodes · <AnimatedNumber value={edgeCount} /> edges
      </span>
      {plan.state === "draft" ? (
        <Button size="xs" disabled={busy} onClick={onApprove}>
          Approve plan
        </Button>
      ) : (
        <span className={cn(PLAN_BADGE, "gap-1 border-status-success bg-(--feedback-success-soft) text-status-success")}>
          <Check size={11} />
          Approved
        </span>
      )}
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            size="icon-xs"
            variant="outline"
            className="bg-surface-raised"
            aria-label="Refresh plan"
            disabled={busy}
            onClick={onRefresh}
          >
            <RotateCcw />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Refresh</TooltipContent>
      </Tooltip>
    </>
  );
}
