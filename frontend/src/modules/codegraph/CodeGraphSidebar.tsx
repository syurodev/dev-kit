import { Button } from "@/components/ui/button";
import { FileTree } from "./FileTree";
import type {
  CodeGraphEndpoint,
  CodeGraphFileEntry,
} from "../../bridge/codegraph";

type CodeGraphSidebarProps = {
  projectPath: string;
  busy: boolean;
  status: string;
  error: string;
  activeTab: "endpoints" | "files";
  onTabChange: (tab: "endpoints" | "files") => void;
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

  return (
    <div className="flex h-full flex-col gap-3 p-3 text-sm">
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
          <span className="text-xs text-muted-foreground truncate font-mono" title={projectPath}>
            {projectPath}
          </span>
        )}
      </div>

      {status && <p className="text-xs text-muted-foreground">{status}</p>}
      {error && <p className="text-xs text-destructive">{error}</p>}

      <input
        type="search"
        value={query}
        onChange={(e) => onQueryChange(e.target.value)}
        placeholder="Search files or endpoints…"
        aria-label="Search files or endpoints"
        className="w-full rounded-md border border-border-subtle bg-surface-tinted px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-tertiary focus-visible:border-ring focus-visible:outline-none"
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
                <span className="font-mono text-xs font-semibold uppercase text-status-info shrink-0">
                  {ep.httpMethod}
                </span>
                <span className="text-xs truncate">{ep.path}</span>
              </button>
            </li>
          ))}
          {shownEndpoints.length === 0 && !busy && (
            <li className="text-xs text-muted-foreground py-2 text-center">
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
    </div>
  );
}
