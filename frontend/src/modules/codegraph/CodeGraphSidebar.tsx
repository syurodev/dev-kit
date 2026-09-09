import { FolderOpen, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { FileTree } from "./FileTree";
import { methodChipClass } from "./graph/tokens";
import { PLAN_BADGE, PLAN_STATUS, planStatusOrder } from "./plan/planStatus";
import { cn } from "@/lib/utils";
import type { CodeGraphPlanProgress } from "../../bridge/codegraphPlan";
import type {
  CodeGraphEndpoint,
  CodeGraphFileEntry,
} from "../../bridge/codegraph";

export type CodeGraphSidebarTab = "endpoints" | "files" | "plan";

type CodeGraphSidebarProps = {
  projectPath: string;
  busy: boolean;
  status: string;
  error: string;
  activeTab: CodeGraphSidebarTab;
  onTabChange: (tab: CodeGraphSidebarTab) => void;
  query: string;
  onQueryChange: (query: string) => void;
  onPickProject: () => void;
  onReindex: () => void;
  endpoints: CodeGraphEndpoint[];
  selectedEndpointId?: string | null;
  onSelectEndpoint: (ep: CodeGraphEndpoint) => void;
  files: CodeGraphFileEntry[];
  selectedFile: string | null;
  onSelectFile: (path: string) => void;
  plan: CodeGraphPlanProgress | null;
  planError: string;
  onSelectPlanNode: (planNodeId: string) => void;
};

/**
 * Figma `CodeGraph / Sidebar` (482:907, 862:33450, 862:33497): a project
 * picker (primary Open + Re-index, the path in Code/11 under them), a 40px
 * search field, a segmented Files / Endpoints / Plan strip, then the list
 * the tab names. Endpoint rows carry a 58×21 method chip; plan rows a status
 * badge over the FQN; files a 28px tree.
 */
export function CodeGraphSidebar(props: CodeGraphSidebarProps) {
  const {
    projectPath,
    busy,
    status,
    error,
    activeTab,
    onTabChange,
    query,
    onQueryChange,
    onPickProject,
    onReindex,
    endpoints,
    selectedEndpointId,
    onSelectEndpoint,
    files,
    selectedFile,
    onSelectFile,
    plan,
    planError,
    onSelectPlanNode,
  } = props;

  // Client-side filter over lists already in memory — the placeholder promises
  // "files or endpoints", which is exactly these two lists, so this needs no
  // bridge call. Symbol search (searchCodeGraphSymbol) is a different feature:
  // it reaches classes and methods that are in the index but in neither list.
  const needle = query.trim().toLowerCase();
  const shownEndpoints = needle
    ? endpoints.filter(
        (ep) =>
          ep.path.toLowerCase().includes(needle) ||
          ep.httpMethod.toLowerCase().includes(needle),
      )
    : endpoints;
  const shownFiles = needle
    ? files.filter((f) => f.path.toLowerCase().includes(needle))
    : files;

  // Diverged first, done last — the sidebar is a worklist, not a plan dump.
  // Filtered by the same needle as the other two tabs, so the search box means
  // one thing everywhere.
  const planNodes = (plan?.nodes ?? [])
    .filter((n) => !needle || n.label.toLowerCase().includes(needle) || n.fqn.toLowerCase().includes(needle))
    .slice()
    .sort((a, b) => planStatusOrder(a.status) - planStatusOrder(b.status));

  return (
    <div className="flex h-full flex-col gap-3.5 py-4">
      <div className="flex flex-col gap-1.5">
        {/* Icon-only, so the row cannot outgrow the sidebar however narrow
            the sash is pulled. Buttons are whitespace-nowrap by design and
            these two labels wanted ~279px against the 224px a minimum-width
            sidebar has; square affordances are 88px and stay 88px.

            Icons alone need somewhere for the words to live, so each carries
            both a tooltip for the mouse and an aria-label for a screen
            reader — the label did not disappear, it moved. */}
        <TooltipProvider>
          <div className="flex items-center gap-2">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  onClick={onPickProject}
                  disabled={busy}
                  size="icon"
                  aria-label="Open Java project"
                >
                  <FolderOpen />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Open Java project…</TooltipContent>
            </Tooltip>
            {projectPath && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    onClick={onReindex}
                    disabled={busy}
                    variant="outline"
                    size="icon"
                    aria-label="Re-index project"
                    className="bg-surface-raised"
                  >
                    <RefreshCw />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Re-index project</TooltipContent>
              </Tooltip>
            )}
          </div>
        </TooltipProvider>
        {projectPath && (
          <span className="truncate text-code-11 text-text-tertiary" title={projectPath}>
            {projectPath}
          </span>
        )}
        {status && <p className="text-caption-11 text-text-tertiary">{status}</p>}
        {error && <p className="text-caption-11 text-status-danger">{error}</p>}
      </div>

      <Input
        type="search"
        value={query}
        onChange={(e) => onQueryChange(e.target.value)}
        placeholder="Search files or endpoints…"
        aria-label="Search files or endpoints"
        className="bg-state-soft"
      />

      <Tabs value={activeTab} onValueChange={(v) => onTabChange(v as CodeGraphSidebarTab)} className="gap-0">
        <TabsList className="w-full">
          <TabsTrigger value="files" className="flex-1">Files</TabsTrigger>
          <TabsTrigger value="endpoints" className="flex-1">Endpoints</TabsTrigger>
          <TabsTrigger value="plan" className="flex-1">Plan</TabsTrigger>
        </TabsList>
      </Tabs>

      {activeTab === "endpoints" && (
        <>
          <span className="text-label-11-caps text-text-tertiary">
            {shownEndpoints.length} endpoint{shownEndpoints.length === 1 ? "" : "s"}
          </span>
          <ul className="-mt-2 flex min-h-0 flex-1 flex-col overflow-y-auto">
            {shownEndpoints.map((ep) => {
              const selected = selectedEndpointId === ep.entryNode;
              return (
                <li key={ep.id}>
                  <button
                    className={cn(
                      "flex w-full items-center gap-2.5 rounded-sm border px-2 py-1.5 text-left",
                      selected
                        ? "border-border-subtle bg-surface-raised"
                        : "border-transparent hover:bg-surface-raised/60",
                    )}
                    onClick={() => onSelectEndpoint(ep)}
                  >
                    <span
                      className={cn(
                        "flex h-5.25 w-14.5 shrink-0 items-center justify-center rounded-sm text-code-11 uppercase",
                        methodChipClass(ep.httpMethod),
                      )}
                    >
                      {ep.httpMethod}
                    </span>
                    <span
                      className={cn(
                        "min-w-0 flex-1 truncate text-body-13",
                        selected ? "text-text-primary" : "text-text-secondary",
                      )}
                    >
                      {ep.path}
                    </span>
                  </button>
                </li>
              );
            })}
            {shownEndpoints.length === 0 && !busy && (
              <li className="py-2 text-center text-body-12 text-text-tertiary">
                No endpoints yet — open a project to index it.
              </li>
            )}
          </ul>
        </>
      )}

      {activeTab === "files" && (
        <div className="min-h-0 flex-1 overflow-hidden">
          <FileTree entries={shownFiles} selectedPath={selectedFile} onSelectFile={onSelectFile} />
        </div>
      )}

      {activeTab === "plan" && (
        <ul className="flex min-h-0 flex-1 flex-col overflow-y-auto">
          {planNodes.map((n) => (
            <li key={n.planNodeId}>
              <button
                className="flex w-full flex-col gap-0.5 rounded-sm px-2 py-1.5 text-left hover:bg-surface-raised/60"
                onClick={() => onSelectPlanNode(n.planNodeId)}
              >
                <span className="flex w-full items-center gap-2">
                  <span className={cn(PLAN_BADGE, PLAN_STATUS[n.status].badge)}>
                    {PLAN_STATUS[n.status].label}
                  </span>
                  <span className="min-w-0 flex-1 truncate text-body-12 text-text-primary">
                    {n.label}
                  </span>
                </span>
                <span className="w-full truncate text-code-11 text-text-tertiary" title={n.fqn}>
                  {n.fqn}
                </span>
              </button>
            </li>
          ))}
          {planNodes.length === 0 && (
            <li className="py-2 text-center text-body-12 text-text-tertiary">
              {planError
                ? "No plan for this project yet."
                : "No plan nodes. An agent creates one with codegraph.proposeFlow."}
            </li>
          )}
        </ul>
      )}
    </div>
  );
}
