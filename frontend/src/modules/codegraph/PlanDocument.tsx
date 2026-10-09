import { useCallback, useEffect, useMemo } from "react";
import { GitFork, RotateCcw, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/ui/chip";
import { EmptyState } from "@/components/ui/empty-state";
import type { CodeGraphEdge, CodeGraphNode } from "@/bridge/codegraph";
import { FlowCanvas } from "./graph/FlowCanvas";
import { shortLabel } from "./graph/labels";
import { PlanHeader, PlanMeta } from "./plan/PlanBar";
import { PlanNodeDetail } from "./plan/PlanNodeDetail";
import { PlanCommentsPopover } from "./plan/PlanCommentsPopover";
import { PLAN_STATUS } from "./plan/planStatus";
import { emptyPlanState, useCodeGraphStore } from "./codegraphStore";
import { approvePlan, loadPlan, loadPlanTasks, relinkPlanNode, setPlanCommentCount } from "./planActions";
import { describeParams, PLAN_VIEW } from "./documents";
import { set } from "./storeAccess";
import { useDocumentOwnership } from "./useDocument";

const onSelectPlanNode = (id: string) => set({ selectedPlanNodeId: id, commentsPlanNodeId: id });
const closeComments = () => set({ commentsPlanNodeId: null });

/**
 * The plan of one folder as a tab: the agent's proposed flow, each node's status
 * against that folder's index. A plan is stored in a folder's index, so the tab
 * names the folder and says so; plans of different folders are separate
 * documents, never one merged list.
 */
export function PlanDocument({ params }: { params?: Record<string, unknown> }) {
  const doc = describeParams(PLAN_VIEW, params);
  const ownership = useDocumentOwnership(doc);
  const workspaceId = ownership.owner?.workspaceId ?? "";
  const folderId = ownership.owner?.folderId ?? "";
  const ready = ownership.relation === "current";
  const state = useCodeGraphStore((s) => (folderId ? s.plans[folderId] : undefined)) ?? emptyPlanState();
  const { plan, edges: planEdges, busy: planBusy, error: planError, commentCounts, tasks: planTasks } = state;
  const selectedPlanNodeId = useCodeGraphStore((s) => s.selectedPlanNodeId);
  const commentsPlanNodeId = useCodeGraphStore((s) => s.commentsPlanNodeId);
  const several = useCodeGraphStore((s) => s.folders.length > 1);
  const retryPlan = useCallback(() => void loadPlan(folderId), [folderId]);

  // Restored after a relaunch, or opened before the sidebar's Plan section
  // ever loaded it.
  useEffect(() => {
    if (ready && !useCodeGraphStore.getState().plans[folderId]?.plan) void loadPlan(folderId);
  }, [ready, folderId]);

  // The chips come from Tasks, which changes without Code Graph hearing about
  // it — a task created on the board, or moved by the reconcile after a
  // re-index. Refreshing them when this tab opens costs one bounded query and
  // keeps the canvas honest.
  useEffect(() => {
    if (ready) void loadPlanTasks(folderId);
  }, [ready, folderId]);

  if (!ready) {
    return (
      <EmptyState
        icon={TriangleAlert}
        title={ownership.relation === "other-workspace" ? "This plan is from another workspace" : "This plan's folder is not in the workspace"}
        body="Switch to its workspace in the title bar to read it."
      />
    );
  }

  // The plan canvas reuses FlowCanvas whole. Plan nodes travel in the same
  // shape as real ones with an empty filePath — a shape the view already
  // handles, since a not_implemented node has no source to show either.
  const planFlowNodes = useMemo<CodeGraphNode[]>(
    () =>
      (plan?.nodes ?? []).map((n) => ({
        id: n.planNodeId,
        label: n.label,
        kind: n.kind,
        layer: "",
        filePath: "",
        line: 0,
        endLine: 0,
        summary: n.description,
      })),
    [plan],
  );
  const planFlowEdges = useMemo(
    () => planEdges.map((e) => ({ from: e.from, to: e.to, kind: "call" })),
    [planEdges],
  );
  const planById = useMemo(
    () => new Map((plan?.nodes ?? []).map((n) => [n.planNodeId, n])),
    [plan],
  );
  const selectedPlanNode = selectedPlanNodeId ? (planById.get(selectedPlanNodeId) ?? null) : null;
  const conditionByEdge = useMemo(
    () => new Map(planEdges.map((e) => [`${e.from}->${e.to}`, e.condition])),
    [planEdges],
  );
  // A root is a node nothing points at. Without one, rankGraph has no place to
  // start; falling back to the first node keeps a cyclic plan renderable.
  const planRootId = useMemo(() => {
    const targets = new Set(planEdges.map((e) => e.to));
    return planFlowNodes.find((n) => !targets.has(n.id))?.id ?? planFlowNodes[0]?.id ?? null;
  }, [planFlowNodes, planEdges]);

  // Stable across unrelated re-renders: FlowCanvas rebuilds its cards when
  // these change. They change only when what the cards say does — a status or
  // a comment count — which updates the cards in place without a re-layout.
  const buildData = useCallback(
    (n: CodeGraphNode) => {
      const p = planById.get(n.id);
      const status = p?.status ?? "not_implemented";
      const comments = commentCounts[n.id] ?? 0;
      const tasks = planTasks[n.id];
      const title = shortLabel(n.label, n.kind);
      return {
        title,
        fqn: p?.fqn ?? "",
        // What the agent called this node when it proposed the flow — an
        // entity, a migration, a dto — not the "class"/"method" structural
        // kind CodeGraphNode.kind carries. inferPlanLayer only recognises
        // Spring's own architectural roles, so this is what a reader has
        // instead of "unknown" for everything else the plan touches.
        kind: p?.kind ?? "",
        status,
        commentCount: comments,
        tasks,
        ariaLabel:
          `${title}, ${PLAN_STATUS[status].label}` +
          (comments ? `, ${comments} comment(s)` : "") +
          (tasks?.length ? `, ${tasks.length} task(s)` : ""),
      };
    },
    [planById, commentCounts, planTasks],
  );
  const edgeLabel = useCallback(
    (e: CodeGraphEdge) => conditionByEdge.get(`${e.from}->${e.to}`) || undefined,
    [conditionByEdge],
  );

  if (plan && planRootId) {
    return (
      <div className="flex h-full w-full flex-col overflow-hidden">
        {selectedPlanNode && (
          <PlanNodeDetail
            node={selectedPlanNode}
            busy={planBusy}
            onRelink={(fqn) => void relinkPlanNode(folderId, selectedPlanNode.planNodeId, fqn)}
          />
        )}
        {/* An approve or relink that failed leaves the plan on screen; the
            error goes above it rather than replacing it. */}
        {planError && (
          <p role="alert" className="px-4 pt-2 text-body-12 text-status-danger">
            {planError}
          </p>
        )}
        <div className="flex-1 overflow-hidden">
          <FlowCanvas
            key={`plan-${planRootId}`}
            rootId={planRootId}
            nodes={planFlowNodes}
            edges={planFlowEdges}
            nodeType="planNode"
            planLegend
            header={
              <>
                <PlanHeader plan={plan} />
                {several ? <Chip className="max-w-40 truncate" >{ownership.label}</Chip> : null}
              </>
            }
            meta={
              <PlanMeta
                plan={plan}
                edgeCount={planEdges.length}
                busy={planBusy}
                onApprove={() => void approvePlan(folderId)}
                onRefresh={retryPlan}
              />
            }
            hint={
              <>
                <strong className="text-text-primary">Click</strong> to open comments
              </>
            }
            buildData={buildData}
            edgeLabel={edgeLabel}
            selectedNodeId={selectedPlanNodeId}
            onSelect={onSelectPlanNode}
            inspector={
              commentsPlanNodeId ? (
                <PlanCommentsPopover
                  workspaceId={workspaceId}
                  folderId={folderId}
                  planNodeId={commentsPlanNodeId}
                  label={planById.get(commentsPlanNodeId)?.fqn ?? commentsPlanNodeId}
                  description={planById.get(commentsPlanNodeId)?.description ?? ""}
                  onClose={closeComments}
                  onCountChange={(nodeId, n) => setPlanCommentCount(folderId, nodeId, n)}
                />
              ) : null
            }
          />
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      {planBusy ? (
        <EmptyState busy title="Loading plan" body="Reading the plan and recomputing each node's status against the index." />
      ) : planError ? (
        <EmptyState icon={TriangleAlert} title="Could not load the plan" body={planError}>
          <Button variant="outline" className="bg-surface-raised" onClick={retryPlan}>
            Retry
            <RotateCcw className="size-4" />
          </Button>
        </EmptyState>
      ) : (
        <EmptyState
          icon={GitFork}
          title={`No plan for ${several ? ownership.label : "this folder"} yet`}
          body="An agent creates one with codegraph.proposeFlow; you approve it here, and every node shows how far the code has caught up."
        />
      )}
    </div>
  );
}
