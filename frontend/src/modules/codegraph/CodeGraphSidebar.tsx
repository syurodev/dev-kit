import { Button } from "@/components/ui/button";
import { FileTree } from "./FileTree";
import { PLAN_STATUS, planStatusOrder } from "./plan/planStatus";
import { cn } from "@/lib/utils";
import type { CodeGraphPlanProgress } from "../../bridge/codegraphPlan";
import type {
  CodeGraphEndpoint,
  CodeGraphFileEntry,
} from "../../bridge/codegraph";

type CodeGraphSidebarProps = {
  projectPath: string;
  busy: boolean;
  status: string;
  error: string;
  activeTab: "endpoints" | "files" | "plan";
  onTabChange: (tab: "endpoints" | "files" | "plan") => void;
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
    <div className="flex h-full flex-col gap-3 p-3 text-body-13">
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <Button onClick={onPickProject} disabled={busy} size="sm" className="flex-1">
            Open Java project…
          </Button>
          {projectPath && (
            <Button onClick={onReindex} disabled={busy} variant="secondary" size="sm">
              Re-index
            </Button>
          )}
        </div>
        {projectPath && (
          <span className="text-code-12 truncate text-muted-foreground" title={projectPath}>
            {projectPath}
          </span>
        )}
      </div>

      {status && <p className="text-body-12 text-muted-foreground">{status}</p>}
      {error && <p className="text-body-12 text-destructive">{error}</p>}

      <input
        type="search"
        value={query}
        onChange={(e) => onQueryChange(e.target.value)}
        placeholder="Search files or endpoints…"
        aria-label="Search files or endpoints"
        className="w-full rounded-md border border-border-subtle bg-surface-tinted px-2.5 py-1.5 text-body-13 text-text-primary placeholder:text-text-tertiary focus-visible:border-ring focus-visible:outline-none"
      />

      <div className="flex items-center gap-1 border-b pb-2">
        <Button
          variant={activeTab === "endpoints" ? "secondary" : "ghost"}
          size="sm"
          className="flex-1"
          onClick={() => onTabChange("endpoints")}
        >
          Endpoints ({shownEndpoints.length})
        </Button>
        <Button
          variant={activeTab === "files" ? "secondary" : "ghost"}
          size="sm"
          className="flex-1"
          onClick={() => onTabChange("files")}
        >
          Files
        </Button>
        <Button
          variant={activeTab === "plan" ? "secondary" : "ghost"}
          size="sm"
          className="flex-1"
          onClick={() => onTabChange("plan")}
        >
          Plan
        </Button>
      </div>

      {activeTab === "endpoints" && (
        <ul className="flex-1 overflow-y-auto space-y-1">
          {shownEndpoints.map((ep) => (
            <li key={ep.id}>
              <button
                className={`w-full text-left rounded px-2 py-1.5 hover:bg-surface-raised transition-colors flex items-center gap-2 ${
                  selectedEndpointId === ep.entryNode ? "bg-surface-raised font-medium" : ""
                }`}
                onClick={() => onSelectEndpoint(ep)}
              >
                <span className="text-code-12-medium uppercase text-status-info shrink-0">
                  {ep.httpMethod}
                </span>
                <span className="text-body-12 truncate">{ep.path}</span>
              </button>
            </li>
          ))}
          {shownEndpoints.length === 0 && !busy && (
            <li className="text-body-12 text-muted-foreground py-2 text-center">
              No endpoints yet — open a project to index it.
            </li>
          )}
        </ul>
      )}

      {activeTab === "files" && (
        <div className="flex-1 overflow-hidden">
          <FileTree entries={shownFiles} selectedPath={selectedFile} onSelectFile={onSelectFile} />
        </div>
      )}

      {activeTab === "plan" && (
        <ul className="flex-1 space-y-1 overflow-y-auto">
          {planNodes.map((n) => (
            <li key={n.planNodeId}>
              <button
                className="flex w-full flex-col gap-0.5 rounded px-2 py-1.5 text-left transition-colors hover:bg-surface-raised"
                onClick={() => onSelectPlanNode(n.planNodeId)}
              >
                <span className="flex items-center gap-2">
                  <span
                    className={cn(
                      "shrink-0 rounded px-1 text-caption-11-medium",
                      PLAN_STATUS[n.status].badge,
                    )}
                  >
                    {PLAN_STATUS[n.status].label}
                  </span>
                  <span className="truncate text-body-12">{n.label}</span>
                </span>
                <span className="truncate text-code-11 text-text-tertiary" title={n.fqn}>
                  {n.fqn}
                </span>
              </button>
            </li>
          ))}
          {planNodes.length === 0 && (
            <li className="py-2 text-center text-body-12 text-muted-foreground">
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
