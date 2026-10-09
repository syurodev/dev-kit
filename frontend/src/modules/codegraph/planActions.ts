import {
  approveFolderPlan,
  countFolderPlanComments,
  getFolderPlanEdges,
  getFolderPlanProgress,
  relinkFolderPlanNode,
} from "@/bridge/workspaceCode";
import { errorMessage } from "@/bridge/errors";
import { emptyPlanState, type PlanState } from "./codegraphStore";
import { isNoPlanError } from "./codegraphErrors";
import { begin, folderById, set, snapshot } from "./storeAccess";
import { documentsOf, openPlanDoc, type DocOwner, type OpenOptions } from "./documents";

/**
 * The plan, by folder.
 *
 * A plan is stored in one folder's index (its plan_nodes sit beside its nodes),
 * so there is no "the workspace's plan": there is the plan of each folder, and
 * the UI says whose it is. Plans are never merged into one list — two folders'
 * nodes could be told apart only by a folder that a merged array would drop.
 */

function patchPlan(folderId: string, patch: Partial<PlanState>) {
  const { plans } = snapshot();
  set({ plans: { ...plans, [folderId]: { ...(plans[folderId] ?? emptyPlanState()), ...patch } } });
}

/** Chooses whose plan the sidebar's Plan tab shows. */
export function choosePlanFolder(folderId: string) {
  set({ planFolderId: folderId, selectedPlanNodeId: null, commentsPlanNodeId: null });
  if (!snapshot().plans[folderId]?.plan) void loadPlan(folderId);
}

/**
 * Loads one folder's plan and its edges together. Status is recomputed by Go
 * on every read, so this is also how the canvas picks up an agent's progress —
 * no diffing here, and nothing cached beyond the last answer.
 */
export async function loadPlan(folderId: string): Promise<void> {
  const { workspaceId } = snapshot();
  if (!folderById(folderId)) return;
  const current = begin(`plan:${folderId}`);
  patchPlan(folderId, { busy: true, error: "" });
  try {
    const [plan, edges, commentCounts] = await Promise.all([
      getFolderPlanProgress(workspaceId, folderId),
      getFolderPlanEdges(workspaceId, folderId),
      countFolderPlanComments(workspaceId, folderId),
    ]);
    if (current()) patchPlan(folderId, { plan, edges, commentCounts });
    // Separate and after: the plan must draw whether or not a board exists.
    void loadPlanTasks(folderId);
  } catch (e) {
    if (!current()) return;
    // Having no plan is the normal state for most folders, not a failure.
    patchPlan(folderId, { plan: null, edges: [], error: isNoPlanError(e) ? "" : errorMessage(e) });
  } finally {
    if (current()) patchPlan(folderId, { busy: false });
  }
}

/** The plan's canvas for a folder as a tab, with `nodeId` picked on it. */
export function showPlan(owner: DocOwner, nodeId?: string, options?: OpenOptions) {
  if (nodeId) set({ selectedPlanNodeId: nodeId });
  openPlanDoc(owner, options);
  if (!snapshot().plans[owner.folderId]?.plan && !snapshot().plans[owner.folderId]?.busy) void loadPlan(owner.folderId);
}

export async function approvePlan(folderId: string): Promise<void> {
  const { workspaceId } = snapshot();
  const current = begin(`plan:${folderId}`);
  patchPlan(folderId, { busy: true, error: "" });
  try {
    await approveFolderPlan(workspaceId, folderId);
  } catch (e) {
    if (current()) patchPlan(folderId, { error: errorMessage(e), busy: false });
    return;
  }
  if (current()) await loadPlan(folderId);
}

export async function relinkPlanNode(folderId: string, planNodeID: string, fqn: string): Promise<void> {
  const { workspaceId } = snapshot();
  const current = begin(`plan:${folderId}`);
  patchPlan(folderId, { busy: true, error: "" });
  try {
    await relinkFolderPlanNode(workspaceId, folderId, planNodeID, fqn);
  } catch (e) {
    if (current()) patchPlan(folderId, { error: errorMessage(e), busy: false });
    return;
  }
  // Status is derived from the FQN, so the whole plan is re-read rather than patched.
  if (current()) await loadPlan(folderId);
}

export function setPlanCommentCount(folderId: string, planNodeId: string, count: number) {
  const plan = snapshot().plans[folderId] ?? emptyPlanState();
  patchPlan(folderId, { commentCounts: { ...plan.commentCounts, [planNodeId]: count } });
}

/** After a folder's index changed: a plan already loaded for it is read again. */
export async function refreshFolderPlan(folderId: string): Promise<void> {
  const shown = snapshot().planFolderId === folderId && snapshot().activeTab === "plan";
  const open = documentsOf().some(({ doc }) => doc.kind === "plan" && doc.folderId === folderId);
  if (shown || open) await loadPlan(folderId);
}

/**
 * Loads the task chips for a folder's plan. Deliberately quiet: a folder with
 * no board is the common case and produces no chips and no error; a Tasks
 * module that fails is not allowed to take the plan down with it.
 */
export async function loadPlanTasks(folderId: string): Promise<void> {
  const folder = folderById(folderId);
  if (!folder) return;
  try {
    const { listBoards, tasksByPlanNode } = await import("@/bridge/tasks");
    const { boardForProject, chipsByPlanNode } = await import("./planTasks");
    const board = boardForProject(await listBoards(), folder.path);
    patchPlan(folderId, { tasks: board ? chipsByPlanNode(await tasksByPlanNode(board.id)) : {} });
  } catch {
    // No chips rather than a broken plan.
    patchPlan(folderId, { tasks: {} });
  }
}
