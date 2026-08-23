import type { PlanAggregate, PlanNodeStatus } from "@/bridge/codegraphPlan";

/**
 * Visual treatment per plan-node status.
 *
 * Deliberately achromatic. The node's layer already spends the four accent
 * colours — status/info, status/success, status/warning and text/tertiary —
 * so status is carried by border style and a neutral badge instead. A green
 * "done" and a "Repository" node would otherwise be the same colour saying two
 * different things. `destructive` is the one exception: it belongs to no layer,
 * and diverged is the one state that has to read as a problem.
 */
export const PLAN_STATUS: Record<
  PlanNodeStatus,
  { label: string; border: string; badge: string; hint: string }
> = {
  not_implemented: {
    label: "Planned",
    border: "border-dashed border-foreground/25",
    badge: "bg-foreground/5 text-foreground/50",
    hint: "No code matches this FQN yet.",
  },
  pending_enhancement: {
    label: "Needs change",
    border: "border-dashed border-foreground/50",
    badge: "bg-foreground/10 text-foreground/70",
    hint: "The method exists but does not yet call what the plan says it should.",
  },
  implemented_matching: {
    label: "Done",
    border: "border-solid border-foreground/20",
    badge: "bg-foreground/10 text-foreground/60",
    hint: "Code matches the plan.",
  },
  implemented_diverged: {
    label: "Diverged",
    border: "border-solid border-2 border-destructive",
    badge: "bg-destructive/15 text-destructive",
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
