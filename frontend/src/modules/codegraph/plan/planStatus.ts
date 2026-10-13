import type { PlanAggregate, PlanNodeStatus } from "@/bridge/codegraphPlan";

/** Figma plan `badge` (862:33608): 16 tall, radius 6, Code/11, a 1px stroke. */
export const PLAN_BADGE =
  "inline-flex h-4 shrink-0 items-center rounded-sm border px-1.5 text-code-11 whitespace-nowrap";

/**
 * Visual treatment per plan-node status, read off Figma's plan sidebar
 * (862:33605) and plan canvas (482:8255).
 *
 * Deliberately achromatic. The node's layer already spends the four accent
 * colours — hue-blue, hue-green, hue-yellow and text/tertiary —
 * so status is carried by border style (dashed = the plan is not satisfied)
 * and a neutral badge. A green "done" and a "Repository" node would otherwise
 * be the same colour saying two different things. `status-danger` is the one
 * exception: it belongs to no layer, and diverged is the one state that has
 * to read as a problem.
 */
export const PLAN_STATUS: Record<
  PlanNodeStatus,
  { label: string; border: string; badge: string; hint: string }
> = {
  not_implemented: {
    label: "Planned",
    border: "border-dashed border-border-subtle",
    badge: "border-border-subtle bg-state-soft text-text-secondary opacity-70",
    hint: "No code matches this FQN yet.",
  },
  pending_enhancement: {
    label: "Needs change",
    border: "border-dashed border-border",
    badge: "border-border bg-state-soft text-text-secondary",
    hint: "The method exists but does not yet call what the plan says it should.",
  },
  implemented_matching: {
    label: "Done",
    border: "border-solid border-border-subtle",
    badge: "border-border-subtle bg-state-soft text-text-secondary",
    hint: "Code matches the plan.",
  },
  implemented_diverged: {
    label: "Diverged",
    border: "border-solid border-2 border-status-danger",
    badge: "border-status-danger bg-(--feedback-danger-soft) text-status-danger",
    hint: "Code exists but its calls do not match the planned edges.",
  },
};

/** Sort key: what needs attention first. */
export function planStatusOrder(status: PlanNodeStatus): number {
  return {
    implemented_diverged: 0,
    pending_enhancement: 1,
    not_implemented: 2,
    implemented_matching: 3,
  }[status];
}

export function aggregateLabel(aggregate: PlanAggregate): string {
  return { not_started: "Not started", in_progress: "In progress", complete: "Complete" }[
    aggregate
  ];
}
