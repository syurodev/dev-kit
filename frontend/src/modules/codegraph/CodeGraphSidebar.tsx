import { useCallback, useMemo } from "react";
import { SidebarHeader, SidebarSearch } from "@/components/ui/sidebar-shell";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { CodeGraphEndpoint, CodeGraphSymbolMatch } from "@/bridge/codegraph";
import { useCodeGraphStore } from "./codegraphStore";
import {
  approvePlan,
  askAgent,
  loadPlan,
  openFile,
  pickAndOpenProject,
  reindex,
  viewFlow,
  viewSymbol,
} from "./codegraphActions";
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
  endpoints: "Search endpoints…   @ symbols",
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
const onAskAboutEndpoint = (ep: CodeGraphEndpoint) =>
  askAgent(`About the endpoint ${ep.httpMethod} ${ep.path} (handled by ${ep.entryNode}) in Code Graph: `);
const onOpenSymbolFile = (m: CodeGraphSymbolMatch) => {
  set({ activeTab: "files", query: "" });
  void openFile(m.filePath, m.line);
};
const onOpenSymbolGraph = (m: CodeGraphSymbolMatch) => {
  set({ query: "" });
  void viewSymbol(m);
};
const onClearQuery = () => set({ query: "" });

/**
 * Figma `CodeGraph / Sidebar` (1192:69479): the project and how fresh its
 * index is, a search that reaches symbols with @, then Endpoints, Files or
 * Plan. Reads the module store itself — everything it shows is module state,
 * and the view only has to register it.
 */
export function CodeGraphSidebar() {
  const projectPath = useCodeGraphStore((s) => s.projectPath);
  const busy = useCodeGraphStore((s) => s.busy);
  const activeTab = useCodeGraphStore((s) => s.activeTab);
  const query = useCodeGraphStore((s) => s.query);
  const endpoints = useCodeGraphStore((s) => s.endpoints);
  const rootEndpointId = useCodeGraphStore((s) => s.rootEndpointId);
  const indexInfo = useCodeGraphStore((s) => s.indexInfo);
  const methodFilter = useCodeGraphStore((s) => s.methodFilter);
  const collapsedGroups = useCodeGraphStore((s) => s.collapsedGroups);
  const files = useCodeGraphStore((s) => s.files);
  const selectedFile = useCodeGraphStore((s) => s.selectedFile);
  const gitStatus = useCodeGraphStore((s) => s.gitStatus);
  const fileFilter = useCodeGraphStore((s) => s.fileFilter);
  const subgraph = useCodeGraphStore((s) => s.subgraph);
  const plan = useCodeGraphStore((s) => s.plan);
  const planBusy = useCodeGraphStore((s) => s.planBusy);
  const planError = useCodeGraphStore((s) => s.planError);
  const selectedPlanNodeId = useCodeGraphStore((s) => s.selectedPlanNodeId);

  const services = useMemo(() => groupEndpoints(endpoints).length, [endpoints]);
  const flowFiles = useMemo(() => new Set((subgraph?.nodes ?? []).map((n) => n.filePath).filter(Boolean)), [subgraph]);
  // The tab's badge: plan nodes that need someone to look.
  const attention = (plan?.nodes ?? []).filter(
    (n) => n.status === "implemented_diverged" || n.status === "pending_enhancement",
  ).length;
  const symbolMode = activeTab === "endpoints" && query.startsWith("@");

  const onSelectPlanNode = useCallback((id: string) => set({ selectedPlanNodeId: id }), []);

  if (!projectPath) {
    return (
      <div className="flex h-full flex-col py-4">
        <EmptyProject busy={busy} onPickProject={pickAndOpenProject} onOpenProject={onOpenProject} />
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col gap-3 py-4">
      <ProjectHeader
        projectPath={projectPath}
        indexInfo={indexInfo}
        counts={{ endpoints: endpoints.length, services }}
        busy={busy}
        onReindex={onReindex}
        onPickProject={pickAndOpenProject}
        onOpenProject={onOpenProject}
      />

      {indexInfo?.error && !busy ? (
        <IndexErrorCard error={indexInfo.error} busy={busy} onRetry={onReindex} onAskAgent={askAgent} />
      ) : null}

      <SidebarHeader>
        <SidebarSearch
          value={query}
          onChange={onQueryChange}
          onClear={onClearQuery}
          placeholder={PLACEHOLDER[activeTab]}
          aria-label={PLACEHOLDER[activeTab].replace(/\s+@ symbols$/, "")}
        />
      </SidebarHeader>

      {symbolMode ? null : (
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
          selectedEndpointId={rootEndpointId}
          methodFilter={methodFilter}
          collapsed={collapsedGroups}
          onMethodFilter={(methods) => set({ methodFilter: methods })}
          onToggleGroup={onToggleGroup}
          onSelect={viewFlow}
          onAskAgent={onAskAboutEndpoint}
        />
      ) : activeTab === "files" ? (
        <FilesTab
          files={files}
          query={query}
          selectedFile={selectedFile}
          gitStatus={gitStatus}
          flowFiles={flowFiles}
          filter={fileFilter}
          onFilter={(next) => set({ fileFilter: next })}
          onSelectFile={(path) => void openFile(path)}
        />
      ) : (
        <PlanTab
          plan={plan}
          query={query}
          busy={planBusy}
          error={planError}
          selectedNodeId={selectedPlanNodeId}
          onRetry={() => void loadPlan()}
          onApprove={() => void approvePlan()}
          onSelectNode={onSelectPlanNode}
        />
      )}
    </div>
  );
}
