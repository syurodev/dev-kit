import { useEffect, useMemo, useState } from "react";
import { Search } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { SidebarHeader, SidebarInset, SidebarSearch } from "@/components/ui/sidebar-shell";
import { EmptyState } from "@/components/ui/empty-state";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { CodeGraphEntryPoint } from "@/bridge/codegraph";
import { dispatchAttachIntent } from "@/workbench/intents";
import { useCodeGraphStore } from "./codegraphStore";
import {
  choosePlanFolder,
  loadPlan,
  makePrimary,
  openFile,
  openSearch,
  reindexFolders,
  setScope,
  showPlan,
  viewFlow,
  viewSymbol,
} from "./codegraphActions";
import { MODULE, describeDocument } from "./documents";
import { set, snapshot } from "./storeAccess";
import { useDocumentOwnership, useStarted } from "./useDocument";
import { WorkspaceHeader, useFolderHealths } from "./sidebar/WorkspaceHeader";
import { EntryPointsTab, type FolderEntries } from "./sidebar/EntryPointsTab";
import { SymbolResults, type SymbolHit } from "./sidebar/SymbolResults";
import { FilesTab, type FilesTabActions } from "./sidebar/FilesTab";
import { SearchTab } from "./sidebar/SearchTab";
import { PlanTab } from "./sidebar/PlanTab";
import { PlanSource } from "./sidebar/PlanSource";
import { OutlineSection } from "./sidebar/OutlineSection";
import { scopeFolders, scopeFolderIds } from "./workspace/scope";
import { docKey, folderLabels } from "./workspace/identity";
import { flowKey } from "./workspace/identity";
import type { FlowFiles } from "./sidebar/filesModel";

export type CodeGraphSidebarTab = "entries" | "files" | "search" | "plan";

const PLACEHOLDER: Record<CodeGraphSidebarTab, string> = {
  // Short enough for the narrowest rail (240): the longer Figma wording was
  // cut mid-word there.
  entries: "Search, or @ for symbols",
  files: "Search files…",
  search: "",
  plan: "Filter plan nodes…",
};

const ownerNow = (folderId: string) => ({ workspaceId: snapshot().workspaceId, folderId });

const onTabChange = (tab: CodeGraphSidebarTab) => {
  set({ activeTab: tab });
  if (tab === "plan") {
    const s = snapshot();
    const inView = scopeFolders(s.scope, s.folders);
    const folderId = inView.some((f) => f.id === s.planFolderId) ? s.planFolderId : (inView[0]?.id ?? "");
    if (folderId) choosePlanFolder(folderId);
  }
  // A search section opened for the first time starts from the sidebar's scope.
  if (tab === "search" && !snapshot().search.options.query) openSearch(snapshot().scope);
};
const onQueryChange = (query: string) => set({ query });
const onToggleGroup = (key: string) => {
  const { collapsedGroups } = snapshot();
  set({ collapsedGroups: collapsedGroups.includes(key) ? collapsedGroups.filter((k) => k !== key) : [...collapsedGroups, key] });
};
// Structured, not pasted text: the ref rides to the agent as a devkit://
// URI it resolves itself through codegraph.getApiFlow when it needs the
// call chain, rather than a sentence frozen at the moment this was clicked
// (docs/dev-kit/cross-module.mdx § Primitive 1). The ref kind stays
// "endpoint" — it is what agents resolve, whatever kind of entry point. The
// project is the folder whose index lists it: an entry point id is its folder's.
const onAskAboutEntryPoint = (folderId: string, ep: CodeGraphEntryPoint) =>
  void dispatchAttachIntent("agent.attach", [
    {
      module: "codegraph",
      kind: "endpoint",
      id: ep.id,
      label: `${ep.verb} ${ep.target}`,
      project: snapshot().folders.find((f) => f.id === folderId)?.path,
    },
  ]);
const onPreviewEntryPoint = (folderId: string, ep: CodeGraphEntryPoint) => viewFlow(ownerNow(folderId), ep);
const onKeepEntryPoint = (folderId: string, ep: CodeGraphEntryPoint) => viewFlow(ownerNow(folderId), ep, { preview: false });
const onEntryPointBeside = (folderId: string, ep: CodeGraphEntryPoint) => viewFlow(ownerNow(folderId), ep, { preview: false, beside: true });
// A search result is a deliberate pick, not browsing: it keeps its tab.
const onOpenSymbolFile = ({ folderId, match }: SymbolHit) => {
  set({ activeTab: "files", query: "" });
  void openFile({ ...ownerNow(folderId), path: match.filePath }, match.line, { preview: false });
};
const onOpenSymbolGraph = ({ folderId, match }: SymbolHit) => {
  set({ query: "" });
  viewSymbol(ownerNow(folderId), match, { preview: false });
};
const onClearQuery = () => set({ query: "" });

const filesActions: FilesTabActions = {
  onFilter: (next) => set({ fileFilter: next }),
  onToggleFolder: (id) => {
    const { fileExpanded } = snapshot();
    set({ fileExpanded: fileExpanded.includes(id) ? fileExpanded.filter((p) => p !== id) : [...fileExpanded, id] });
  },
  onToggleRoot: (folderId) => {
    const { collapsedRoots } = snapshot();
    set({ collapsedRoots: collapsedRoots.includes(folderId) ? collapsedRoots.filter((p) => p !== folderId) : [...collapsedRoots, folderId] });
  },
  onSelectFile: (folderId, path) => void openFile({ ...ownerNow(folderId), path }, null, { preview: true }),
  onKeepFile: (folderId, path) => void openFile({ ...ownerNow(folderId), path }, null, { preview: false }),
  onOpenBeside: (folderId, path) => void openFile({ ...ownerNow(folderId), path }, null, { preview: false, beside: true }),
  // A context menu on a folder searches that folder, whatever the sidebar's scope is.
  onFindInFolder: (folderId) => openSearch({ kind: "folders", ids: [folderId] }),
  onShowOnly: (folderId) => setScope({ kind: "folders", ids: [folderId] }),
  onReindexFolder: (folderId) => void reindexFolders([folderId]),
  onMakePrimary: (folderId) => void makePrimary(folderId),
  onCopyPath: (text) => void navigator.clipboard?.writeText(text).catch(() => undefined),
  onResetFilters: () => set({ fileFilter: { codeOnly: true, changed: false, inFlow: false }, query: "" }),
};

/**
 * Figma `CodeGraph / Sidebar`, for a workspace: the workspace and what is in
 * view, a search that reaches symbols with @, then Entry points, Files, Search or
 * Plan. Reads the module store itself — everything it shows is module state, and
 * the view only has to register it.
 *
 * Nothing here chooses "the project". The lists are lenses over the workspace's
 * folders (the header's scope); a click opens a document that names its own
 * folder; the primary is only a badge.
 */
export function CodeGraphSidebar() {
  useStarted();
  // What the rail marks is the document in front: the flow's entry point, the file.
  const front = useWorkbenchTabsStore((s) => {
    const tabs = moduleTabs(s.modules, MODULE);
    return tabs.openTabs.find((t) => t.id === tabs.activeTabId) ?? null;
  });
  const doc = describeDocument(front);
  const ownership = useDocumentOwnership(doc);

  const workspaceId = useCodeGraphStore((s) => s.workspaceId);
  const foldersStatus = useCodeGraphStore((s) => s.foldersStatus);
  const foldersError = useCodeGraphStore((s) => s.foldersError);
  const folders = useCodeGraphStore((s) => s.folders);
  const primaryId = useCodeGraphStore((s) => s.primaryId);
  const scope = useCodeGraphStore((s) => s.scope);
  const data = useCodeGraphStore((s) => s.data);
  const activeTab = useCodeGraphStore((s) => s.activeTab);
  const query = useCodeGraphStore((s) => s.query);
  const verbFilter = useCodeGraphStore((s) => s.verbFilter);
  const collapsedGroups = useCodeGraphStore((s) => s.collapsedGroups);
  const fileFilter = useCodeGraphStore((s) => s.fileFilter);
  const fileExpanded = useCodeGraphStore((s) => s.fileExpanded);
  const collapsedRoots = useCodeGraphStore((s) => s.collapsedRoots);
  const planFolderId = useCodeGraphStore((s) => s.planFolderId);
  const plans = useCodeGraphStore((s) => s.plans);
  const selectedPlanNodeId = useCodeGraphStore((s) => s.selectedPlanNodeId);
  // The rail needs dirty badges, not editor text. Further keystrokes in an
  // already-dirty buffer must not rebuild the file tree.
  const dirtyKeys = useCodeGraphStore(useShallow((s) => Object.keys(s.buffers).filter((key) => s.buffers[key].dirty)));
  const flowSubgraph = useCodeGraphStore((s) =>
    doc.kind === "flow" && doc.folderId && doc.root ? s.flows[flowKey(doc.folderId, doc.root)]?.subgraph : undefined,
  );
  const [outlineOpen, setOutlineOpen] = useState(true);

  const labels = useMemo(() => folderLabels(folders.map((f) => ({ id: f.id, name: f.name, path: f.path }))), [folders]);
  const inView = useMemo(() => scopeFolders(scope, folders), [scope, folders]);
  const inViewIds = useMemo(() => scopeFolderIds(scope, folders), [scope, folders]);
  const unsaved = useMemo(() => new Set(dirtyKeys), [dirtyKeys]);
  const healths = useFolderHealths();
  const healthOf = useMemo(() => new Map(healths.map((h) => [h.id, h.health])), [healths]);

  // The rail marks the front document only when it is of the open workspace.
  const selectedFile =
    doc.kind === "file" && ownership.relation === "current" && ownership.owner && doc.path ? { folderId: ownership.owner.folderId, path: doc.path } : null;
  const flow: FlowFiles = useMemo(() => {
    if (!flowSubgraph || !doc.folderId) return null;
    return { folderId: doc.folderId, files: new Set(flowSubgraph.nodes.map((n) => n.filePath).filter(Boolean)) };
  }, [flowSubgraph, doc.folderId]);
  const selectedEntry =
    doc.kind === "flow" && doc.folderId
      ? { folderId: doc.folderId, entryPointId: doc.entryPoint ?? data[doc.folderId]?.entries?.entries.find((ep) => ep.entryNode === doc.root)?.id ?? "" }
      : null;

  const entryFolders: FolderEntries[] = useMemo(
    () =>
      inView.map((f) => {
        const d = data[f.id];
        const health = healthOf.get(f.id) ?? { health: "pending" as const, tone: "neutral" as const, label: "Checking", detail: "" };
        let note = "";
        if (health.health === "unsupported") note = "Files only — no analyzer reads this folder";
        else if (health.health === "failed" || health.health === "needs-analyzer") note = health.label;
        else if (health.health === "indexing" || health.health === "pending") note = d?.entries?.entries.length ? "" : "Indexing…";
        else if (health.health === "not-indexed") note = "Not indexed yet";
        else if (health.health === "unavailable") note = health.detail;
        return { folderId: f.id, path: f.path, label: labels[f.id], entries: d?.entries?.entries ?? [], health, note };
      }),
    [inView, data, healthOf, labels],
  );

  const attention = useMemo(
    () => (plans[planFolderId]?.plan?.nodes ?? []).filter((n) => n.status === "implemented_diverged" || n.status === "pending_enhancement").length,
    [plans, planFolderId],
  );
  const tab: CodeGraphSidebarTab = activeTab;
  const symbolMode = tab === "entries" && query.startsWith("@");

  useEffect(() => {
    // The Plan section follows the scope: a folder that left it is not the source any more.
    if (tab === "plan" && inView.length > 0 && !inView.some((f) => f.id === planFolderId)) choosePlanFolder(inView[0].id);
  }, [tab, inView, planFolderId]);

  if (!workspaceId) {
    return (
      <div className="flex h-full flex-col select-none">
        <SidebarInset>
          <EmptyState size="rail" title="No workspace selected" body="Pick or create a workspace from the switcher in the title bar. Code Graph reads every folder of it." />
        </SidebarInset>
      </div>
    );
  }
  if (foldersStatus === "error") {
    return (
      <div className="flex h-full flex-col select-none">
        <SidebarInset>
          <EmptyState size="rail" tone="danger" title="Could not read the workspace" body={foldersError} />
        </SidebarInset>
      </div>
    );
  }
  if (foldersStatus !== "ready") {
    return (
      <div className="flex h-full flex-col select-none">
        <SidebarInset>
          <EmptyState size="rail" busy title="Reading the workspace" body="Listing its folders…" />
        </SidebarInset>
      </div>
    );
  }
  if (folders.length === 0) {
    return (
      <div className="flex h-full flex-col select-none">
        <SidebarInset>
          <EmptyState size="rail" title="This workspace has no folders yet" body="Add one from Edit workspace in the title bar's workspace menu. Folders are added and removed there, not here." />
        </SidebarInset>
      </div>
    );
  }

  const planFolder = plans[planFolderId];
  return (
    <div className="flex h-full min-h-0 flex-col gap-3 select-none">
      <SidebarInset>
        <WorkspaceHeader />
      </SidebarInset>

      {tab === "search" ? null : (
        <SidebarHeader>
          <SidebarSearch
            value={query}
            onChange={onQueryChange}
            onClear={onClearQuery}
            placeholder={PLACEHOLDER[tab]}
            aria-label={tab === "entries" ? "Search entry points, or type @ for classes and methods" : PLACEHOLDER[tab]}
          />
        </SidebarHeader>
      )}

      {symbolMode ? null : (
        <SidebarInset>
          <Tabs value={tab} onValueChange={(v) => onTabChange(v as CodeGraphSidebarTab)} className="gap-0">
            <TabsList className="w-full">
              <TabsTrigger value="entries" className="flex-1" title="Entry points">
                Entries
              </TabsTrigger>
              <TabsTrigger value="files" className="flex-1">
                Files
              </TabsTrigger>
              <Tooltip>
                <TooltipTrigger asChild>
                  <TabsTrigger value="search" className="flex-none px-2.5" aria-label="Find in workspace">
                    <Search className="size-3.5" aria-hidden />
                  </TabsTrigger>
                </TooltipTrigger>
                <TooltipContent>Find in workspace</TooltipContent>
              </Tooltip>
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
          workspaceId={workspaceId}
          folderIds={inViewIds}
          labels={labels}
          query={query.slice(1)}
          onOpenGraph={onOpenSymbolGraph}
          onOpenFile={onOpenSymbolFile}
          onClear={onClearQuery}
        />
      ) : tab === "entries" ? (
        <EntryPointsTab
          folders={entryFolders}
          query={query}
          selected={selectedEntry}
          verbFilter={verbFilter}
          collapsed={collapsedGroups}
          onVerbFilter={(verbs) => set({ verbFilter: verbs })}
          onToggleGroup={onToggleGroup}
          onSelect={onPreviewEntryPoint}
          onKeep={onKeepEntryPoint}
          onOpenBeside={onEntryPointBeside}
          onAskAgent={onAskAboutEntryPoint}
          onReindexFolder={(folderId) => void reindexFolders([folderId])}
        />
      ) : tab === "files" ? (
        <>
          <FilesTab
            folders={inView}
            labels={labels}
            primaryId={primaryId}
            data={data}
            query={query}
            selected={selectedFile}
            unsaved={unsaved}
            workspaceId={workspaceId}
            flow={flow}
            filter={fileFilter}
            expanded={fileExpanded}
            collapsedRoots={collapsedRoots}
            actions={filesActions}
          />
          {selectedFile && doc.path ? (
            <OutlineSection
              docKey={docKey(workspaceId, selectedFile.folderId, doc.path)}
              workspaceId={workspaceId}
              folderId={selectedFile.folderId}
              path={doc.path}
              open={outlineOpen}
              onToggle={() => setOutlineOpen((v) => !v)}
            />
          ) : null}
        </>
      ) : tab === "search" ? (
        <SearchTab workspaceId={workspaceId} />
      ) : (
        <>
          <PlanSource folders={inView} labels={labels} selectedId={planFolderId} onSelect={choosePlanFolder} />
          <PlanTab
            plan={planFolder?.plan ?? null}
            query={query}
            busy={planFolder?.busy ?? false}
            error={planFolder?.error ?? ""}
            selectedNodeId={selectedPlanNodeId}
            onRetry={() => planFolderId && void loadPlan(planFolderId)}
            onSelectNode={(id) => planFolderId && showPlan(ownerNow(planFolderId), id)}
            onOpenCanvas={() => planFolderId && showPlan(ownerNow(planFolderId), undefined, { preview: false })}
          />
        </>
      )}
    </div>
  );
}
