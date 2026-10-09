import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  CodeGraphPlanComment,
  CodeGraphPlanEdge,
  CodeGraphPlanProgress,
  PlanAggregate,
} from "./codegraphPlan";

/**
 * The plan lives in one folder's index, so these name the folder
 * (internal/bridge/workspace_code.go). They are the by-folder form of the
 * focused-project calls in codegraphPlan.ts, which are kept for the agent path.
 */

export async function getFolderPlanProgress(workspaceId: string, folderId: string): Promise<CodeGraphPlanProgress> {
  const p = await callBridge(() => AppService.WorkspacePlanProgress(workspaceId, folderId));
  return {
    description: p.description,
    state: p.state as "draft" | "approved",
    aggregate: p.aggregate as PlanAggregate,
    nodes: (p.nodes ?? []) as CodeGraphPlanProgress["nodes"],
  };
}

export async function getFolderPlanEdges(workspaceId: string, folderId: string): Promise<CodeGraphPlanEdge[]> {
  const plan = await callBridge(() => AppService.WorkspacePlanFlow(workspaceId, folderId));
  return (plan.edges ?? []) as CodeGraphPlanEdge[];
}

export function approveFolderPlan(workspaceId: string, folderId: string): Promise<void> {
  return callBridge(() => AppService.WorkspacePlanApprove(workspaceId, folderId) as Promise<void>);
}

export function relinkFolderPlanNode(workspaceId: string, folderId: string, planNodeId: string, fqn: string): Promise<void> {
  return callBridge(() => AppService.WorkspacePlanRelink(workspaceId, folderId, planNodeId, fqn) as Promise<void>);
}

export async function listFolderPlanComments(workspaceId: string, folderId: string, planNodeId: string): Promise<CodeGraphPlanComment[]> {
  return ((await callBridge(() => AppService.WorkspacePlanComments(workspaceId, folderId, planNodeId))) ?? []) as CodeGraphPlanComment[];
}

export function commentFolderPlanNode(workspaceId: string, folderId: string, planNodeId: string, body: string): Promise<void> {
  return callBridge(() => AppService.WorkspacePlanComment(workspaceId, folderId, planNodeId, body) as Promise<void>);
}

export async function countFolderPlanComments(workspaceId: string, folderId: string): Promise<Record<string, number>> {
  return ((await callBridge(() => AppService.WorkspacePlanCommentCounts(workspaceId, folderId))) ?? {}) as Record<string, number>;
}
