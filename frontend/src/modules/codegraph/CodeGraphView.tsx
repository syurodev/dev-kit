import { useEffect, useMemo } from "react";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { useSplitPanel } from "@/components/ui/split-panel";
import { AnalyzerSetup } from "./AnalyzerSetup";
import { CodeGraphSidebar, type CodeGraphSidebarTab } from "./CodeGraphSidebar";
import { NodeInspector } from "./inspector/NodeInspector";
import { EndpointsPane } from "./EndpointsPane";
import { PlanPane } from "./PlanPane";
import { FilesPane } from "./FilesPane";
import { onCodeGraphIndexUpdated, onCodeGraphProjectChanged } from "../../bridge/codegraph";
import { useCodeGraphStore } from "./codegraphStore";
import {
  closeInspector,
  handleProjectChanged,
  inspectNode,
  loadAnalyzerStatus,
  loadPlan,
  openFile,
  pickAndOpenProject,
  refreshAfterReindex,
  reindex,
  restoreProject,
  viewFlow,
} from "./codegraphActions";

// Reads and writes go through the module store rather than useState — see
// codegraphStore.ts for why the view cannot own this state. The actions that
// talk to the bridge live in codegraphActions.ts; each tab's canvas in its own
// pane file.
const set = useCodeGraphStore.getState().set;
const snapshot = () => useCodeGraphStore.getState();

const onTabChange = (tab: CodeGraphSidebarTab) => {
  set({ activeTab: tab });
  if (tab === "plan") void loadPlan();
};
const onQueryChange = (q: string) => set({ query: q });
const onReindex = () => reindex(snapshot().projectPath);
const onSelectFile = (path: string) => void openFile(path);
const onSelectPlanNode = (id: string) => set({ selectedPlanNodeId: id });
const onRetryPlan = () => void loadPlan();

export function CodeGraphView() {
  const projectPath = useCodeGraphStore((s) => s.projectPath);
  const endpoints = useCodeGraphStore((s) => s.endpoints);
  const subgraph = useCodeGraphStore((s) => s.subgraph);
  const rootEndpointId = useCodeGraphStore((s) => s.rootEndpointId);
  const status = useCodeGraphStore((s) => s.status);
  const error = useCodeGraphStore((s) => s.error);
  const busy = useCodeGraphStore((s) => s.busy);
  const activeTab = useCodeGraphStore((s) => s.activeTab);
  const files = useCodeGraphStore((s) => s.files);
  const selectedFile = useCodeGraphStore((s) => s.selectedFile);
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
  const planBusy = useCodeGraphStore((s) => s.planBusy);
  const planError = useCodeGraphStore((s) => s.planError);
  const snippet = useCodeGraphStore((s) => s.snippet);
  const snippetLoading = useCodeGraphStore((s) => s.snippetLoading);
  const snippetError = useCodeGraphStore((s) => s.snippetError);

  // Subscribed once: the handlers read live state through the store, so they
  // never need to re-subscribe and cannot capture a stale closure.
  useEffect(() => onCodeGraphIndexUpdated(() => void refreshAfterReindex()), []);
  // The project can change without this view doing it — an agent switching it
  // over MCP. Everything project-scoped is reset when it does.
  useEffect(() => onCodeGraphProjectChanged((root) => void handleProjectChanged(root)), []);

  // Probe once per mount. Cheap enough there and the answer cannot change
  // without someone installing a JDK, which is not something to poll for.
  useEffect(() => {
    void loadAnalyzerStatus();
    let cancelled = false;
    restoreProject(() => cancelled).catch(() => {
      /* A locked vault will restore on the next mount. */
    });
    return () => {
      cancelled = true;
    };
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
                set({ activeTab: "files" });
                closeInspector();
              }
            : undefined
        }
        onJump={(id) => void inspectNode(id)}
      />,
      {
        width: 520,
        onClose: closeInspector,
      },
    );
    return () => closePanel();
  }, [inspectorNode, snippet, snippetLoading, snippetError, openPanel, closePanel]);

  useSidebarContent(
    <CodeGraphSidebar
      projectPath={projectPath}
      busy={busy}
      status={status}
      error={error}
      activeTab={activeTab}
      onTabChange={onTabChange}
      query={query}
      onQueryChange={onQueryChange}
      onPickProject={pickAndOpenProject}
      onReindex={onReindex}
      endpoints={endpoints}
      selectedEndpointId={rootEndpointId}
      onSelectEndpoint={viewFlow}
      files={files}
      selectedFile={selectedFile}
      onSelectFile={onSelectFile}
      plan={plan}
      planBusy={planBusy}
      planError={planError}
      onRetryPlan={onRetryPlan}
      onSelectPlanNode={onSelectPlanNode}
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
      {activeTab === "endpoints" && <EndpointsPane />}
      {activeTab === "plan" && <PlanPane />}
      {activeTab === "files" && <FilesPane />}
    </div>
  );
}
