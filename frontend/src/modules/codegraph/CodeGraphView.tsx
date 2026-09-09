import { useEffect, useMemo } from "react";
import { FileCode2, FolderSearch, GitFork, Loader2, RotateCcw, TriangleAlert } from "lucide-react";
import { Dialogs } from "@wailsio/runtime";
import { keymap } from "@codemirror/view";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { Button } from "@/components/ui/button";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { useSplitPanel } from "@/components/ui/split-panel";
import { AnalyzerSetup } from "./AnalyzerSetup";
import { CodeGraphSidebar } from "./CodeGraphSidebar";
import { BlastRadiusPanel } from "./BlastRadiusPanel";
import { NodeInspector } from "./inspector/NodeInspector";
import { PlanHeader, PlanMeta } from "./plan/PlanBar";
import { PlanNodeDetail } from "./plan/PlanNodeDetail";
import { PlanCommentsPopover } from "./plan/PlanCommentsPopover";
import { languageExtensionForPath } from "./editorLanguage";
import { FlowCanvas } from "./graph/FlowCanvas";
import { EmptyProgress, EmptyState } from "@/components/ui/empty-state";
import { shortLabel } from "./graph/labels";
import { methodChipClass } from "./graph/tokens";
import { cn } from "@/lib/utils";
import {
  codeGraphAnalyzerStatus,
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
import { errorMessage, toAppError } from "../../bridge/errors";
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

/**
 * Refreshes which analyzers are built.
 *
 * Called on mount and after a build, never on a timer: it probes the machine
 * for a JDK and Maven, which costs a subprocess apiece. A probe that fails
 * leaves the statuses null, which reads as "assume fine" — a broken status call
 * must not hide a working module behind a setup panel.
 */
async function loadAnalyzerStatus() {
  try {
    set({ analyzerStatuses: await codeGraphAnalyzerStatus() });
  } catch {
    set({ analyzerStatuses: null });
  }
}

/**
 * What the sidebar says after an index.
 *
 * A monorepo indexes as one graph, so without a module count four services
 * look exactly like one project that happens to have a lot of endpoints. Said
 * only when there is more than one — "1 module" is noise on the common case.
 *
 * Exported for its own test: it is the one piece of this file that is a pure
 * function of two numbers, and the alternative was driving a native folder
 * picker to check a string.
 */
export function indexedStatus(moduleCount: number, endpointCount: number): string {
  const modules = moduleCount > 1 ? `${moduleCount} modules · ` : "";
  return `Indexed. ${modules}${endpointCount} endpoint(s) found.`;
}

async function reindex(path: string) {
  set({
    busy: true,
    status: "Indexing…",
    error: "",
    errorCode: "",
    subgraph: null,
    rootId: null,
  });
  try {
    const detected = await openCodeGraphProject(path);
    const list = await listCodeGraphEndpoints();
    set({ endpoints: list });
    await loadFiles();
    set({ status: indexedStatus(detected.modules.length, list.length) });
  } catch (e) {
    const failure = toAppError(e);
    set({ endpoints: [], files: [], status: "", error: failure.message, errorCode: failure.code });
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
  const errorCode = useCodeGraphStore((s) => s.errorCode);
  const busy = useCodeGraphStore((s) => s.busy);
  const activeTab = useCodeGraphStore((s) => s.activeTab);
  const files = useCodeGraphStore((s) => s.files);
  const selectedFile = useCodeGraphStore((s) => s.selectedFile);
  const fileContent = useCodeGraphStore((s) => s.fileContent);
  const fileDirty = useCodeGraphStore((s) => s.fileDirty);
  const fileError = useCodeGraphStore((s) => s.fileError);
  const loadingMore = useCodeGraphStore((s) => s.loadingMore);
  const query = useCodeGraphStore((s) => s.query);
  const analyzerStatuses = useCodeGraphStore((s) => s.analyzerStatuses);
  // Null (not probed, or the probe failed) counts as ready: a status call that
  // cannot answer must not hide a working module behind a setup panel.
  const missingAnalyzers = (analyzerStatuses ?? []).filter((s) => !s.ready);
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
  const rootEndpoint = useMemo(
    () => endpoints.find((ep) => ep.entryNode === rootId) ?? null,
    [endpoints, rootId],
  );
  const snippet = useCodeGraphStore((s) => s.snippet);
  const snippetLoading = useCodeGraphStore((s) => s.snippetLoading);
  const snippetError = useCodeGraphStore((s) => s.snippetError);

  // Subscribed once: the handler reads live state through snapshot(), so it
  // never needs to re-subscribe and cannot capture a stale closure.
  useEffect(() => onCodeGraphIndexUpdated(() => void refreshAfterReindex()), []);

  // Probe once per mount. Cheap enough there and the answer cannot change
  // without someone installing a JDK, which is not something to poll for.
  useEffect(() => {
    void loadAnalyzerStatus();
  }, []);

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

  // Nothing in CodeGraph works without an analyzer, so setup takes the whole
  // canvas rather than sitting in a corner of a module that cannot do anything.
  // The sidebar is still mounted above, so the project picker stays reachable.
  if (missingAnalyzers.length > 0) {
    return (
      <AnalyzerSetup
        statuses={missingAnalyzers}
        onBuilt={() => {
          void loadAnalyzerStatus();
          // The project the user already chose is still selected; index it now
          // rather than making them pick it again.
          const path = snapshot().projectPath;
          if (path) void reindex(path);
        }}
      />
    );
  }

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      {activeTab === "endpoints" && (
        <div className="flex flex-1 flex-col overflow-hidden">
          {subgraph && rootId ? (
            <FlowCanvas
              key={rootId}
              rootId={rootId}
              nodes={subgraph.nodes}
              edges={subgraph.edges}
              onSelect={(nodeId) => void inspectNode(nodeId)}
              header={
                rootEndpoint ? (
                  <>
                    <span
                      className={cn(
                        "rounded-sm px-2 py-0.75 text-code-12-medium uppercase",
                        methodChipClass(rootEndpoint.httpMethod),
                      )}
                    >
                      {rootEndpoint.httpMethod}
                    </span>
                    <span className="truncate text-title-16 text-text-primary">{rootEndpoint.path}</span>
                  </>
                ) : (
                  <span className="truncate text-title-16 text-text-primary">{rootId}</span>
                )
              }
              meta={
                <>
                  <span className="text-code-11 text-text-tertiary">
                    {subgraph.nodes.length} nodes · {subgraph.edges.length} edges
                  </span>
                  {subgraph.truncated && (
                    <>
                      <span
                        title="The backend capped this subgraph; some nodes are not shown yet."
                        className="text-code-11 text-status-warning"
                      >
                        partial
                      </span>
                      <Button
                        variant="outline"
                        size="xs"
                        className="bg-surface-raised"
                        disabled={loadingMore || !subgraph.continuation}
                        onClick={() => void loadMoreNeighbors()}
                      >
                        {loadingMore ? "Loading…" : "Load more"}
                      </Button>
                    </>
                  )}
                </>
              }
              hint={
                <>
                  <strong className="text-text-primary">Click</strong> to open code
                </>
              }
            />
          ) : busy ? (
            <EmptyState
              icon={Loader2}
              title="Analyzing codebase"
              body="Indexing controllers, services and repositories. The graph opens as soon as the first route resolves."
            >
              <EmptyProgress />
            </EmptyState>
          ) : error && errorCode === "unsupported_project" ? (
            /* Nothing was analyzed and nothing is wrong with the analyzer —
               the folder simply is not a project. Offering a retry here would
               be a button that cannot work; the only useful move is choosing
               somewhere else. */
            <EmptyState icon={FolderSearch} title="Nothing to index in that folder" body={error}>
              <Button onClick={pickAndOpenProject}>Choose another folder…</Button>
            </EmptyState>
          ) : error ? (
            <EmptyState icon={TriangleAlert} title="Could not analyze this project" body={error}>
              <Button variant="outline" className="bg-surface-raised" onClick={() => reindex(projectPath)} disabled={!projectPath}>
                Re-run analysis
                <RotateCcw className="size-4" />
              </Button>
            </EmptyState>
          ) : (
            <EmptyState
              icon={GitFork}
              title={projectPath ? "Pick an endpoint to graph" : "Open a Java project"}
              body={
                projectPath
                  ? "Choose a route on the left and DevKit traces its controller, services and repositories from the analyzed codebase."
                  : "DevKit indexes the project's controllers, services and repositories, then draws the call chain behind each route."
              }
            >
              {projectPath ? (
                <span className="flex items-center gap-2 rounded-md bg-state-soft px-2.5 py-1.25 text-body-12 text-text-tertiary">
                  <span className="text-code-11">⌘K</span>
                  search endpoints
                </span>
              ) : (
                <Button onClick={pickAndOpenProject}>Open Java project…</Button>
              )}
            </EmptyState>
          )}
        </div>
      )}

      {activeTab === "plan" && (
        <div className="flex flex-1 flex-col overflow-hidden">
          {plan && planRootId ? (
            <>
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
                  planLegend
                  header={<PlanHeader plan={plan} />}
                  meta={
                    <PlanMeta
                      plan={plan}
                      edgeCount={planEdges.length}
                      busy={planBusy}
                      onApprove={() => void approvePlan()}
                      onRefresh={() => void loadPlan()}
                    />
                  }
                  hint={
                    <>
                      <strong className="text-text-primary">Click</strong> to open comments
                    </>
                  }
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
          ) : planBusy ? (
            <EmptyState icon={Loader2} title="Loading plan" body="Reading the plan and recomputing each node's status against the index.">
              <EmptyProgress />
            </EmptyState>
          ) : (
            <EmptyState
              icon={GitFork}
              title="No plan for this project yet"
              body="An agent creates one with codegraph.proposeFlow; you approve it here, and every node shows how far the code has caught up."
            />
          )}
        </div>
      )}

      {activeTab === "files" && (
        <div className="flex flex-1 flex-col overflow-hidden">
          {selectedFile ? (
            <div className="flex flex-1 flex-col overflow-hidden">
              <div className="flex h-9 items-center justify-between border-b border-border-subtle px-6">
                <span className="truncate text-code-11 text-text-secondary">{selectedFile}</span>
                {fileDirty && <span className="text-code-11 text-status-warning">unsaved</span>}
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
            <EmptyState
              icon={FileCode2}
              title="Pick a file to edit"
              body="Select a file from the sidebar to open it here. ⌘S saves it back to disk; Impact lists what else depends on it."
            />
          )}
          {selectedFile && (
            <BlastRadiusPanel key={selectedFile} filePath={selectedFile} onOpenFile={openFile} />
          )}
          {fileError && <p className="px-6 py-1 text-body-12 text-status-danger">{fileError}</p>}
        </div>
      )}
    </div>
  );
}
