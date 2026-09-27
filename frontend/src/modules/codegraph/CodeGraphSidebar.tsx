import { useEffect, useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { SidebarHeader, SidebarInset, SidebarSearch } from "@/components/ui/sidebar-shell";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { CodeGraphEndpoint, CodeGraphSymbolMatch } from "@/bridge/codegraph";
import { useCodeGraphStore } from "./codegraphStore";
import {
  askAgent,
  ensureStarted,
  loadPlan,
  openFile,
  reindex,
  showPlan,
  viewFlow,
  viewSymbol,
} from "./codegraphActions";
import { dispatchAttachIntent } from "@/workbench/intents";
import { MODULE, describeDocument } from "./documents";
import { ProjectHeader } from "./sidebar/ProjectHeader";
import { EmptyProject } from "./sidebar/EmptyProject";
import { IndexErrorCard } from "./sidebar/IndexErrorCard";
import { EndpointsTab } from "./sidebar/EndpointsTab";
import { SymbolResults } from "./sidebar/SymbolResults";
import { FilesTab } from "./sidebar/FilesTab";
import { PlanTab } from "./sidebar/PlanTab";
import { groupEndpoints } from "./sidebar/sidebarModel";

export type CodeGraphSidebarTab = "endpoints" | "files" | "plan";

const set = useCodeGraphStore.getState().set;
const snapshot = () => useCodeGraphStore.getState();

const PLACEHOLDER: Record<CodeGraphSidebarTab, string> = {
  // Short enough for the narrowest rail (240): the longer Figma wording was
  // cut mid-word there.
  endpoints: "Search, or @ for symbols",
  files: "Search files…",
  plan: "Filter plan nodes…",
};

const onTabChange = (tab: CodeGraphSidebarTab) => {
  set({ activeTab: tab });
  if (tab === "plan") void loadPlan();
};
const onQueryChange = (query: string) => set({ query });
const onReindex = () => void reindex(snapshot().projectPath);
const onOpenProject = (path: string) => void reindex(path);
const onToggleGroup = (key: string) => {
  const { collapsedGroups } = snapshot();
  set({ collapsedGroups: collapsedGroups.includes(key) ? collapsedGroups.filter((k) => k !== key) : [...collapsedGroups, key] });
};
// Structured, not pasted text: the ref rides to the agent as a devkit://
// URI it resolves itself through codegraph.getApiFlow when it needs the
// call chain, rather than a sentence frozen at the moment this was clicked
// (docs/dev-kit/cross-module.mdx § Primitive 1).
const onAskAboutEndpoint = (ep: CodeGraphEndpoint) =>
  void dispatchAttachIntent("agent.attach", [
    {
      module: "codegraph",
      kind: "endpoint",
      id: ep.id,
      label: `${ep.httpMethod} ${ep.path}`,
      project: snapshot().projectPath,
    },
  ]);
const onPreviewEndpoint = (ep: CodeGraphEndpoint) => viewFlow(ep);
const onKeepEndpoint = (ep: CodeGraphEndpoint) => viewFlow(ep, { preview: false });
const onEndpointBeside = (ep: CodeGraphEndpoint) => viewFlow(ep, { preview: false, beside: true });
// A search result is a deliberate pick, not browsing: it keeps its tab.
const onOpenSymbolFile = (m: CodeGraphSymbolMatch) => {
  set({ activeTab: "files", query: "" });
  openFile(m.filePath, m.line, { preview: false });
};
const onOpenSymbolGraph = (m: CodeGraphSymbolMatch) => {
  set({ query: "" });
  viewSymbol(m, { preview: false });
};
const onClearQuery = () => set({ query: "" });
// Module-level, so the trees' node lists are rebuilt only when their data is.
const onToggleFolder = (path: string) => {
  const { fileExpanded } = snapshot();
  set({ fileExpanded: fileExpanded.includes(path) ? fileExpanded.filter((p) => p !== path) : [...fileExpanded, path] });
};
const onSelectFile = (path: string) => openFile(path);
const onKeepFile = (path: string) => openFile(path, null, { preview: false });
const onSelectPlanNode = (id: string) => showPlan(id);
const onOpenPlan = () => showPlan(undefined, { preview: false });

/**
 * Figma `CodeGraph / Sidebar` (1192:69479): the project and how fresh its
 * index is, a search that reaches symbols with @, then Endpoints, Files or
 * Plan. Reads the module store itself — everything it shows is module state,
 * and the view only has to register it.
 */
export function CodeGraphSidebar() {
  useEffect(ensureStarted, []);
  // What the rail marks is the document in front: the flow's endpoint, the file.
  const front = useWorkbenchTabsStore((s) => {
    const tabs = moduleTabs(s.modules, MODULE);
    return tabs.openTabs.find((t) => t.id === tabs.activeTabId) ?? null;
  });
  const doc = describeDocument(front);
  const projectPath = useCodeGraphStore((s) => s.projectPath);
  const busy = useCodeGraphStore((s) => s.busy);
  const activeTab = useCodeGraphStore((s) => s.activeTab);
  const query = useCodeGraphStore((s) => s.query);
  const endpoints = useCodeGraphStore((s) => s.endpoints);
  const indexInfo = useCodeGraphStore((s) => s.indexInfo);
  const methodFilter = useCodeGraphStore((s) => s.methodFilter);
  const collapsedGroups = useCodeGraphStore((s) => s.collapsedGroups);
  const files = useCodeGraphStore((s) => s.files);
  const gitStatus = useCodeGraphStore((s) => s.gitStatus);
  const fileFilter = useCodeGraphStore((s) => s.fileFilter);
  const fileExpanded = useCodeGraphStore((s) => s.fileExpanded);
  const subgraph = useCodeGraphStore((s) => (doc.root ? s.flows[doc.root]?.subgraph : undefined));
  // The rail needs dirty badges, not editor text. Further keystrokes in an
  // already-dirty buffer must not rebuild the file tree.
  const dirtyPaths = useCodeGraphStore(useShallow((s) =>
    Object.keys(s.buffers).filter((path) => s.buffers[path].dirty),
  ));
  const plan = useCodeGraphStore((s) => s.plan);
  const planBusy = useCodeGraphStore((s) => s.planBusy);
  const planError = useCodeGraphStore((s) => s.planError);
  const selectedPlanNodeId = useCodeGraphStore((s) => s.selectedPlanNodeId);

  const services = useMemo(() => groupEndpoints(endpoints).length, [endpoints]);
  const flowFiles = useMemo(() => new Set((subgraph?.nodes ?? []).map((n) => n.filePath).filter(Boolean)), [subgraph]);
  const unsaved = useMemo(
    () => new Set(dirtyPaths),
    [dirtyPaths],
  );
  // A flow opened from @ has no endpoint; the endpoint it starts at is still it.
  const selectedEndpointId =
    doc.endpoint ?? (doc.root ? endpoints.find((ep) => ep.entryNode === doc.root)?.id : undefined) ?? null;
  // The tab's badge: plan nodes that need someone to look.
  const attention = (plan?.nodes ?? []).filter(
    (n) => n.status === "implemented_diverged" || n.status === "pending_enhancement",
  ).length;
  const symbolMode = activeTab === "endpoints" && query.startsWith("@");

  if (!projectPath) {
    return (
      <div className="flex h-full flex-col select-none">
        <SidebarInset><EmptyProject busy={busy} onOpenProject={onOpenProject} /></SidebarInset>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 select-none">
      <SidebarInset>
        <ProjectHeader
          projectPath={projectPath}
          indexInfo={indexInfo}
          counts={{ endpoints: endpoints.length, services }}
          busy={busy}
          onReindex={onReindex}
          onOpenProject={onOpenProject}
        />
      </SidebarInset>

      {indexInfo?.error && !busy ? (
        <SidebarInset><IndexErrorCard error={indexInfo.error} busy={busy} onRetry={onReindex} onAskAgent={askAgent} /></SidebarInset>
      ) : null}

      <SidebarHeader>
        <SidebarSearch
          value={query}
          onChange={onQueryChange}
          onClear={onClearQuery}
          placeholder={PLACEHOLDER[activeTab]}
          aria-label={activeTab === "endpoints" ? "Search endpoints, or type @ for classes and methods" : PLACEHOLDER[activeTab]}
        />
      </SidebarHeader>

      {symbolMode ? null : (
        <SidebarInset>
          <Tabs value={activeTab} onValueChange={(v) => onTabChange(v as CodeGraphSidebarTab)} className="gap-0">
            <TabsList className="w-full">
              <TabsTrigger value="endpoints" className="flex-1">
                Endpoints
              </TabsTrigger>
              <TabsTrigger value="files" className="flex-1">
                Files
              </TabsTrigger>
              <TabsTrigger value="plan" className="flex-1 gap-1.5">
                Plan
                {attention > 0 ? (
                  <span
                    aria-label={`${attention} need attention`}
                    className="inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full bg-status-danger px-1 text-code-11 text-action-primary-foreground"
                  >
                    {attention}
                  </span>
                ) : null}
              </TabsTrigger>
            </TabsList>
          </Tabs>
        </SidebarInset>
      )}

      {symbolMode ? (
        <SymbolResults
          query={query.slice(1)}
          onOpenGraph={onOpenSymbolGraph}
          onOpenFile={onOpenSymbolFile}
          onClear={onClearQuery}
        />
      ) : activeTab === "endpoints" ? (
        <EndpointsTab
          endpoints={endpoints}
          query={query}
          busy={busy}
          selectedEndpointId={selectedEndpointId}
          methodFilter={methodFilter}
          collapsed={collapsedGroups}
          onMethodFilter={(methods) => set({ methodFilter: methods })}
          onToggleGroup={onToggleGroup}
          onSelect={onPreviewEndpoint}
          onKeep={onKeepEndpoint}
          onOpenBeside={onEndpointBeside}
          onAskAgent={onAskAboutEndpoint}
        />
      ) : activeTab === "files" ? (
        <FilesTab
          files={files}
          query={query}
          selectedFile={doc.kind === "file" ? (doc.path ?? null) : null}
          unsaved={unsaved}
          gitStatus={gitStatus}
          flowFiles={flowFiles}
          filter={fileFilter}
          onFilter={(next) => set({ fileFilter: next })}
          expanded={fileExpanded}
          onToggleFolder={onToggleFolder}
          onSelectFile={onSelectFile}
          onKeepFile={onKeepFile}
        />
      ) : (
        <PlanTab
          plan={plan}
          query={query}
          busy={planBusy}
          error={planError}
          selectedNodeId={selectedPlanNodeId}
          onRetry={() => void loadPlan()}
          onSelectNode={onSelectPlanNode}
          onOpenCanvas={onOpenPlan}
        />
      )}
    </div>
  );
}
