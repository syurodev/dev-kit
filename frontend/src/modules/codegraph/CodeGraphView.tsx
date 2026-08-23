import { useEffect, useMemo } from "react";
import { Dialogs } from "@wailsio/runtime";
import { keymap } from "@codemirror/view";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { Button } from "@/components/ui/button";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { useSplitPanel } from "@/components/ui/split-panel";
import { CodeGraphSidebar } from "./CodeGraphSidebar";
import { BlastRadiusPanel } from "./BlastRadiusPanel";
import { NodeInspector } from "./inspector/NodeInspector";
import { PlanBar } from "./plan/PlanBar";
import { PlanNodeDetail } from "./plan/PlanNodeDetail";
import { PlanCommentsPopover } from "./plan/PlanCommentsPopover";
import { languageExtensionForPath } from "./editorLanguage";
import { FlowCanvas } from "./graph/FlowCanvas";
import { shortLabel } from "./graph/labels";
import {
  openCodeGraphProject,
  listCodeGraphEndpoints,
  getCodeGraphNeighbors,
  getCodeGraphCodeSnippet,
  listCodeGraphFiles,
  onCodeGraphIndexUpdated,
  readCodeGraphFile,
  writeCodeGraphFile,
  type CodeGraphEndpoint,
} from "../../bridge/codegraph";
import {
  approveCodeGraphPlan,
  countCodeGraphPlanNodeComments,
  relinkCodeGraphPlanNode,
  getCodeGraphPlanEdges,
  getCodeGraphPlanProgress,
} from "../../bridge/codegraphPlan";
import { errorMessage } from "../../bridge/errors";
import { useCodeGraphStore } from "./codegraphStore";

// Reads and writes go through the module store rather than useState — see
// codegraphStore.ts for why the view cannot own this state.
const set = useCodeGraphStore.getState().set;
const snapshot = () => useCodeGraphStore.getState();

async function loadFiles() {
  try {
    set({ files: await listCodeGraphFiles() });
  } catch (e) {
    set({ fileError: errorMessage(e) });
  }
}

async function reindex(path: string) {
  set({ busy: true, status: "Indexing…", error: "", subgraph: null, rootId: null });
  try {
    await openCodeGraphProject(path);
    const list = await listCodeGraphEndpoints();
    set({ endpoints: list });
    await loadFiles();
    set({ status: `Indexed. ${list.length} endpoint(s) found.` });
  } catch (e) {
    set({ endpoints: [], files: [], status: "", error: errorMessage(e) });
  } finally {
    set({ busy: false });
  }
}

async function pickAndOpenProject() {
  set({ error: "" });
  let chosen: string;
  try {
    chosen = (await Dialogs.OpenFile({
      CanChooseDirectories: true,
      CanChooseFiles: false,
      Title: "Open Java project",
    })) as string;
  } catch (e) {
    set({ error: errorMessage(e) });
    return;
  }
  if (!chosen) {
    return; // user cancelled the picker
  }
  set({ projectPath: chosen });
  await reindex(chosen);
}

const NEIGHBOR_DEPTH = 3;

async function viewFlow(endpoint: CodeGraphEndpoint) {
  set({ error: "", inspectorNodeId: null, snippet: null, snippetError: "" });
  try {
    const sub = await getCodeGraphNeighbors(endpoint.entryNode, NEIGHBOR_DEPTH);
    set({ subgraph: sub, rootId: endpoint.entryNode });
  } catch (e) {
    set({ subgraph: null, error: errorMessage(e) });
  }
}

/**
 * Fetches the next page of a truncated subgraph and merges it in. Without
 * this, a graph over the backend's ceiling silently rendered a partial
 * picture — GetNeighbors returned `truncated` and a continuation token that
 * nothing read.
 */
async function loadMoreNeighbors() {
  const { rootId, subgraph, loadingMore } = snapshot();
  if (!rootId || !subgraph?.continuation || loadingMore) return;
  set({ loadingMore: true });
  try {
    const next = await getCodeGraphNeighbors(rootId, NEIGHBOR_DEPTH, {
      continuation: subgraph.continuation,
    });
    const byId = new Map(subgraph.nodes.map((n) => [n.id, n]));
    for (const n of next.nodes) byId.set(n.id, n);
    const edgeKey = (e: { from: string; to: string; kind: string }) =>
      `${e.from}\u0000${e.to}\u0000${e.kind}`;
    const edges = new Map(subgraph.edges.map((e) => [edgeKey(e), e]));
    for (const e of next.edges) edges.set(edgeKey(e), e);
    set({
      subgraph: {
        nodes: [...byId.values()],
        edges: [...edges.values()],
        truncated: next.truncated,
        continuation: next.continuation,
      },
    });
  } catch (e) {
    set({ error: errorMessage(e) });
  } finally {
    set({ loadingMore: false });
  }
}

/** Click a node → open its inspector, anchored to that node on the canvas. */
async function inspectNode(nodeId: string) {
  const { subgraph } = snapshot();
  const node = subgraph?.nodes.find((n) => n.id === nodeId);
  if (!node) return;
  set({ inspectorNodeId: nodeId, snippet: null, snippetError: "" });
  // A node with no source yet (a plan node still not_implemented) has an empty
  // filePath — let the Detail tab show its own empty state rather than asking
  // Go for a file that does not exist.
  if (!node.filePath) {
    set({ snippetLoading: false });
    return;
  }
  set({ snippetLoading: true });
  try {
    const snippet = await getCodeGraphCodeSnippet(node.filePath, node.line, node.endLine);
    set({ snippet, snippetLoading: false });
  } catch (e) {
    set({ snippetError: errorMessage(e), snippetLoading: false });
  }
}

/**
 * Loads the plan and its edges together.
 *
 * Status is recomputed by Go on every read, so this is also how the canvas
 * picks up an agent's progress — no diffing here, and nothing cached.
 */
async function loadPlan() {
  set({ planBusy: true, planError: "" });
  try {
    const [plan, planEdges, commentCounts] = await Promise.all([
      getCodeGraphPlanProgress(),
      getCodeGraphPlanEdges(),
      countCodeGraphPlanNodeComments(),
    ]);
    set({ plan, planEdges, commentCounts });
  } catch (e) {
    // Having no plan is the normal state for most projects, not a failure
    // worth shouting about — the empty state says what creates one.
    set({ plan: null, planEdges: [], planError: errorMessage(e) });
  } finally {
    set({ planBusy: false });
  }
}

async function approvePlan() {
  set({ planBusy: true, planError: "" });
  try {
    await approveCodeGraphPlan();
  } catch (e) {
    set({ planError: errorMessage(e) });
  } finally {
    set({ planBusy: false });
  }
  await loadPlan();
}

async function relinkPlanNode(planNodeID: string, fqn: string) {
  set({ planBusy: true, planError: "" });
  try {
    await relinkCodeGraphPlanNode(planNodeID, fqn);
  } catch (e) {
    set({ planError: errorMessage(e) });
  } finally {
    set({ planBusy: false });
  }
  // Status is derived from the FQN, so the whole plan is re-read rather than
  // patched — the relinked node may now resolve, and its edges with it.
  await loadPlan();
}

/**
 * Re-reads whatever is currently on screen after a background re-index.
 *
 * Everything here is derived from the index, so it goes stale silently — plan
 * status especially, since Go recomputes the diff on every read and the UI
 * would otherwise keep showing the answer from before the agent's last commit.
 *
 * The open file is deliberately NOT re-read: that buffer may hold unsaved
 * edits, and refreshing it would throw them away to fix a staleness the user
 * can see and control.
 */
async function refreshAfterReindex() {
  const { rootId, activeTab } = snapshot();
  try {
    set({ endpoints: await listCodeGraphEndpoints() });
    await loadFiles();
    if (rootId) {
      set({ subgraph: await getCodeGraphNeighbors(rootId, NEIGHBOR_DEPTH) });
    }
  } catch (e) {
    set({ error: errorMessage(e) });
  }
  if (activeTab === "plan") {
    await loadPlan();
  }
}

async function openFile(path: string) {
  set({ fileError: "" });
  try {
    const content = await readCodeGraphFile(path);
    set({ selectedFile: path, fileContent: content, fileDirty: false });
  } catch (e) {
    set({ fileError: errorMessage(e) });
  }
}

// Reads the live values off the store rather than through refs mirrored during
// render (fileContentRef.current = fileContent), which the keymap needed back
// when this state was local and which is a render-phase side effect.
async function saveCurrentFile() {
  const { selectedFile, fileContent } = snapshot();
  if (!selectedFile) return;
  try {
    await writeCodeGraphFile(selectedFile, fileContent);
    set({ fileDirty: false });
  } catch (e) {
    set({ fileError: errorMessage(e) });
  }
}

const saveKeymap = keymap.of([
  {
    key: "Mod-s",
    run: () => {
      void saveCurrentFile();
      return true;
    },
  },
]);

export function CodeGraphView() {
  const projectPath = useCodeGraphStore((s) => s.projectPath);
  const endpoints = useCodeGraphStore((s) => s.endpoints);
  const subgraph = useCodeGraphStore((s) => s.subgraph);
  const rootId = useCodeGraphStore((s) => s.rootId);
  const status = useCodeGraphStore((s) => s.status);
  const error = useCodeGraphStore((s) => s.error);
  const busy = useCodeGraphStore((s) => s.busy);
  const activeTab = useCodeGraphStore((s) => s.activeTab);
  const files = useCodeGraphStore((s) => s.files);
  const selectedFile = useCodeGraphStore((s) => s.selectedFile);
  const fileContent = useCodeGraphStore((s) => s.fileContent);
  const fileDirty = useCodeGraphStore((s) => s.fileDirty);
  const fileError = useCodeGraphStore((s) => s.fileError);
  const loadingMore = useCodeGraphStore((s) => s.loadingMore);
  const query = useCodeGraphStore((s) => s.query);
  const inspectorNodeId = useCodeGraphStore((s) => s.inspectorNodeId);
  const inspectorNode = useMemo(
    () => subgraph?.nodes.find((n) => n.id === inspectorNodeId) ?? null,
    [subgraph, inspectorNodeId],
  );
  const plan = useCodeGraphStore((s) => s.plan);
  const planEdges = useCodeGraphStore((s) => s.planEdges);
  const planBusy = useCodeGraphStore((s) => s.planBusy);
  const planError = useCodeGraphStore((s) => s.planError);
  const selectedPlanNodeId = useCodeGraphStore((s) => s.selectedPlanNodeId);
  const commentsPlanNodeId = useCodeGraphStore((s) => s.commentsPlanNodeId);
  const commentCounts = useCodeGraphStore((s) => s.commentCounts);

  // The plan canvas reuses FlowCanvas whole. Plan nodes travel in the same
  // shape as real ones with an empty filePath — a shape the view already
  // handles, since a not_implemented node has no source to show either.
  const planFlowNodes = useMemo(
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
  const snippet = useCodeGraphStore((s) => s.snippet);
  const snippetLoading = useCodeGraphStore((s) => s.snippetLoading);
  const snippetError = useCodeGraphStore((s) => s.snippetError);

  // Subscribed once: the handler reads live state through snapshot(), so it
  // never needs to re-subscribe and cannot capture a stale closure.
  useEffect(() => onCodeGraphIndexUpdated(() => void refreshAfterReindex()), []);

  const { openPanel, closePanel } = useSplitPanel();

  useEffect(() => {
    if (!inspectorNode) {
      closePanel();
      return;
    }
    openPanel(
      <NodeInspector
        node={inspectorNode}
        snippet={snippet}
        snippetLoading={snippetLoading}
        snippetError={snippetError}
        onEdit={
          snippet
            ? () => {
                void openFile(snippet.filePath);
                set({ activeTab: "files", inspectorNodeId: null, snippet: null });
              }
            : undefined
        }
        onJump={(id) => void inspectNode(id)}
      />,
      {
        width: 520,
        onClose: () => set({ inspectorNodeId: null, snippet: null, snippetError: "" }),
      },
    );
    return () => closePanel();
  }, [inspectorNode, snippet, snippetLoading, snippetError, openPanel, closePanel]);

  const editorExtensions = useMemo(
    () => (selectedFile ? [...languageExtensionForPath(selectedFile), saveKeymap] : []),
    [selectedFile],
  );

  useSidebarContent(
    <CodeGraphSidebar
      projectPath={projectPath}
      busy={busy}
      status={status}
      error={error}
      activeTab={activeTab}
      onTabChange={(tab) => {
        set({ activeTab: tab });
        if (tab === "plan") void loadPlan();
      }}
      query={query}
      onQueryChange={(q) => set({ query: q })}
      onPickProject={pickAndOpenProject}
      onReindex={() => reindex(projectPath)}
      endpoints={endpoints}
      selectedEndpointId={rootId}
      onSelectEndpoint={viewFlow}
      files={files}
      selectedFile={selectedFile}
      onSelectFile={openFile}
      plan={plan}
      planError={planError}
      onSelectPlanNode={(id) => set({ selectedPlanNodeId: id })}
    />,
  );

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      {activeTab === "endpoints" && (
        <div className="flex flex-1 flex-col overflow-hidden">
          {subgraph && rootId ? (
            <div className="flex flex-1 flex-col overflow-hidden">
              <div className="flex items-center gap-2 px-3 pt-2">
                <span className="font-mono text-[11px] text-text-tertiary">
                  {subgraph.nodes.length} nodes · {subgraph.edges.length} edges
                </span>
                {subgraph.truncated && (
                  <>
                    <span
                      title="The backend capped this subgraph; some nodes are not shown yet."
                      className="font-mono text-[11px] text-status-warning"
                    >
                      partial
                    </span>
                    <Button
                      variant="secondary"
                      size="sm"
                      className="h-6 px-2 text-xs"
                      disabled={loadingMore || !subgraph.continuation}
                      onClick={() => void loadMoreNeighbors()}
                    >
                      {loadingMore ? "Loading…" : "Load more"}
                    </Button>
                  </>
                )}
              </div>
              <div className="flex-1 overflow-hidden">
                <FlowCanvas
                  key={rootId}
                  rootId={rootId}
                  nodes={subgraph.nodes}
                  edges={subgraph.edges}
                  onSelect={(nodeId) => void inspectNode(nodeId)}
                />
              </div>
            </div>
          ) : (
            <div className="flex h-full items-center justify-center p-4 text-sm text-muted-foreground">
              Select an endpoint from the sidebar to view its flow graph.
            </div>
          )}
        </div>
      )}

      {activeTab === "plan" && (
        <div className="flex flex-1 flex-col overflow-hidden">
          {plan && planRootId ? (
            <>
              <PlanBar
                plan={plan}
                busy={planBusy}
                onApprove={() => void approvePlan()}
                onRefresh={() => void loadPlan()}
              />
              {selectedPlanNode && (
                <PlanNodeDetail
                  node={selectedPlanNode}
                  busy={planBusy}
                  onRelink={(fqn) => void relinkPlanNode(selectedPlanNode.planNodeId, fqn)}
                />
              )}
              <div className="flex-1 overflow-hidden">
                <FlowCanvas
                  key={`plan-${planRootId}`}
                  rootId={planRootId}
                  nodes={planFlowNodes}
                  edges={planFlowEdges}
                  nodeType="planNode"
                  buildData={(n) => {
                    const p = planById.get(n.id);
                    return {
                      title: shortLabel(n.label, n.kind),
                      fqn: p?.fqn ?? "",
                      status: p?.status ?? "not_implemented",
                      commentCount: commentCounts[n.id] ?? 0,
                    };
                  }}
                  edgeLabel={(e) => conditionByEdge.get(`${e.from}->${e.to}`) || undefined}
                  selectedNodeId={selectedPlanNodeId}
                  onSelect={(id) => set({ selectedPlanNodeId: id, commentsPlanNodeId: id })}
                  inspector={
                    commentsPlanNodeId ? (
                      <PlanCommentsPopover
                        planNodeId={commentsPlanNodeId}
                        label={planById.get(commentsPlanNodeId)?.fqn ?? commentsPlanNodeId}
                        onClose={() => set({ commentsPlanNodeId: null })}
                        onCountChange={(n) =>
                          set({
                            commentCounts: {
                              ...snapshot().commentCounts,
                              [commentsPlanNodeId]: n,
                            },
                          })
                        }
                      />
                    ) : null
                  }
                />
              </div>
            </>
          ) : (
            <div className="flex h-full items-center justify-center p-4 text-center text-sm text-muted-foreground">
              {planBusy
                ? "Loading plan…"
                : "No plan for this project yet. An agent creates one with codegraph.proposeFlow; you approve it here."}
            </div>
          )}
        </div>
      )}

      {activeTab === "files" && (
        <div className="flex flex-1 flex-col overflow-hidden">
          {selectedFile ? (
            <div className="flex flex-1 flex-col overflow-hidden">
              <div className="flex items-center justify-between border-b px-3 py-1.5 text-sm text-muted-foreground">
                <span className="truncate font-mono">{selectedFile}</span>
                {fileDirty && <span className="text-status-warning text-xs font-mono">unsaved</span>}
              </div>
              <div className="flex-1 overflow-hidden">
                <CodeEditor
                  value={fileContent}
                  onChange={(v) => set({ fileContent: v, fileDirty: true })}
                  extensions={editorExtensions}
                />
              </div>
            </div>
          ) : (
            <div className="flex h-full items-center justify-center p-4 text-sm text-muted-foreground">
              Select a file from the sidebar to edit it. Ctrl/Cmd+S saves.
            </div>
          )}
          {selectedFile && (
            <BlastRadiusPanel key={selectedFile} filePath={selectedFile} onOpenFile={openFile} />
          )}
          {fileError && <p className="px-3 py-1 text-xs text-destructive">{fileError}</p>}
        </div>
      )}
    </div>
  );
}
