import { RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { CodeGraphPlanProgress } from "@/bridge/codegraphPlan";
import { PLAN_BADGE, PLAN_STATUS } from "../plan/planStatus";
import { planSummary } from "./sidebarModel";

/**
 * The plan as a worklist (Figma `CodeGraph / Sidebar — 4 Plan`): a progress
 * card, then what needs attention, what is planned, what is done. A row opens
 * the plan's canvas on that node.
 *
 * The card's button opens the canvas rather than approving from here: an
 * approval is a judgement on the flow, and the flow is on the canvas — the
 * Approve button is there, beside what it approves.
 */
export function PlanTab({
  plan,
  query,
  busy,
  error,
  selectedNodeId,
  onRetry,
  onOpenCanvas,
  onSelectNode,
}: {
  plan: CodeGraphPlanProgress | null;
  query: string;
  busy: boolean;
  error: string;
  selectedNodeId: string | null;
  onRetry: () => void;
  onOpenCanvas: () => void;
  onSelectNode: (id: string) => void;
}) {
  if (!plan) {
    if (error) {
      // A failed read is not "no plan": saying so would hide a broken plan
      // store behind the normal empty state.
      return (
        <div role="alert" className="flex flex-col items-center gap-2 px-2 py-3 text-center text-body-12">
          <span className="text-status-danger">Could not load the plan: {error}</span>
          <Button size="xs" variant="outline" className="bg-surface-raised" onClick={onRetry} disabled={busy}>
            Retry
            <RotateCcw />
          </Button>
        </div>
      );
    }
    return (
      <p className="px-2 py-3 text-center text-body-12 text-text-tertiary">
        {busy ? "Loading plan…" : "No plan for this folder yet. An agent drafts one with codegraph.proposeFlow."}
      </p>
    );
  }

  const s = planSummary(plan, query);
  const pct = (n: number) => `${s.total ? (n / s.total) * 100 : 0}%`;
  const legend = [
    `${s.done} done`,
    s.needsChange ? `${s.needsChange} needs change` : null,
    s.diverged ? `${s.diverged} diverged` : null,
    s.planned ? `${s.planned} planned` : null,
  ].filter(Boolean);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex shrink-0 flex-col gap-2 rounded-lg border border-border-subtle bg-surface-raised p-2.5">
        <div className="flex items-center gap-1.5">
          <span className="min-w-0 flex-1 truncate text-title-13 text-text-primary" title={plan.description}>
            {s.title}
          </span>
          <span className="shrink-0 text-code-11 text-text-secondary">
            {s.done} / {s.total}
          </span>
        </div>
        <div className="flex h-1 overflow-hidden rounded-full bg-state-soft" aria-hidden>
          <span className="bg-status-success" style={{ width: pct(s.done) }} />
          <span className="bg-status-warning" style={{ width: pct(s.needsChange) }} />
          <span className="bg-status-danger" style={{ width: pct(s.diverged) }} />
        </div>
        <span className="text-caption-11 text-text-tertiary">{legend.join(" · ")}</span>
        {plan.state === "draft" ? (
          <Button size="sm" className="w-full" onClick={onOpenCanvas}>
            Review &amp; approve plan
          </Button>
        ) : (
          <div className="flex items-center justify-between gap-2">
            <span className="min-w-0 truncate text-caption-11 text-text-tertiary">Approved — agents build against it.</span>
            <Button size="xs" variant="ghost" className="shrink-0" onClick={onOpenCanvas}>
              Open canvas
            </Button>
          </div>
        )}
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto">
        {s.sections.map((section) => (
          <section key={section.key} className="flex flex-col gap-0.5">
            <h3 className="flex items-center gap-1.5 px-2 pb-1 text-label-11-caps text-text-tertiary">
              {section.label}
              <span className="text-code-11">{section.nodes.length}</span>
            </h3>
            {section.nodes.map((n) => (
              <button
                key={n.planNodeId}
                type="button"
                onClick={() => onSelectNode(n.planNodeId)}
                className={cn(
                  "flex w-full flex-col gap-0.5 rounded-md border px-2 py-1.5 text-left",
                  selectedNodeId === n.planNodeId ? "border-border-subtle bg-surface-raised" : "border-transparent hover:bg-state-soft",
                )}
              >
                <span className="flex w-full items-center gap-2">
                  <span className={cn(PLAN_BADGE, PLAN_STATUS[n.status].badge)}>{PLAN_STATUS[n.status].label}</span>
                  <span className="min-w-0 flex-1 truncate text-body-13 text-text-primary">{n.label}</span>
                </span>
                <span className="w-full truncate text-code-11 text-text-tertiary" title={n.fqn}>
                  {n.fqn}
                </span>
              </button>
            ))}
          </section>
        ))}
        {s.sections.length === 0 ? (
          <p className="px-2 py-3 text-center text-body-12 text-text-tertiary">
            {query.trim() ? "No plan nodes match." : "This plan has no nodes."}
          </p>
        ) : null}
      </div>
    </div>
  );
}
