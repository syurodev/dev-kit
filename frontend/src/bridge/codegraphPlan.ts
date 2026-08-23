import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type PlanNodeStatus =
  | "not_implemented"
  | "pending_enhancement"
  | "implemented_matching"
  | "implemented_diverged";

export type PlanAggregate = "not_started" | "in_progress" | "complete";

export type CodeGraphPlanNode = {
  planNodeId: string;
  label: string;
  description: string;
  fqn: string;
  kind: "class" | "method";
  status: PlanNodeStatus;
  /** The indexed node the FQN resolved to; empty when not_implemented. */
  realNodeId: string;
};

export type CodeGraphPlanEdge = {
  from: string;
  to: string;
  /** Plan-time documentation for the human reviewing the canvas. Never
   *  machine-verified against real code — see codegraph.mdx § "Điều kiện trên
   *  cạnh". */
  condition: string;
};

export type CodeGraphPlanProgress = {
  description: string;
  state: "draft" | "approved";
  aggregate: PlanAggregate;
  nodes: CodeGraphPlanNode[];
};

/**
 * Per-node status plus the whole-plan aggregate, recomputed on every read —
 * status is never stored, so this is always the current diff against the index.
 */
export async function getCodeGraphPlanProgress(): Promise<CodeGraphPlanProgress> {
  const p = await callBridge(() => AppService.CodeGraphGetPlanProgress());
  return {
    description: p.description,
    state: p.state as "draft" | "approved",
    aggregate: p.aggregate as PlanAggregate,
    nodes: (p.nodes ?? []) as CodeGraphPlanNode[],
  };
}

/** Edges live on the plan itself — PlanProgress carries only nodes. */
export async function getCodeGraphPlanEdges(): Promise<CodeGraphPlanEdge[]> {
  const plan = await callBridge(() => AppService.CodeGraphGetPlannedFlow());
  return (plan.edges ?? []) as CodeGraphPlanEdge[];
}

/**
 * Approving is human-only, like KafkaConnect or SSHTrustHostKey — there is no
 * MCP tool for it, and there should not be.
 */
export async function approveCodeGraphPlan(): Promise<void> {
  return callBridge(() => AppService.CodeGraphApprovePlan());
}

/** Repoint one plan node at a different FQN, when the agent named it otherwise. */
export async function relinkCodeGraphPlanNode(planNodeId: string, fqn: string): Promise<void> {
  return callBridge(() => AppService.CodeGraphRelinkPlanNode(planNodeId, fqn));
}

// Deliberately not wrapped: CodeGraphProposeFlow. A plan is something an agent
// proposes over MCP and a human approves here; a UI "propose" button would
// erase that split. The bridge method exists for completeness — the frontend
// simply has no business calling it.
