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
    onPickProject,
    onReindex,
    endpoints,
    selectedEndpointId,
    onSelectEndpoint,
    files,
    selectedFile,
    onSelectFile,
  } = props;

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

      <div className="flex items-center gap-1 border-b pb-2">
        <Button
          variant={activeTab === "endpoints" ? "secondary" : "ghost"}
          size="sm"
          className="flex-1"
          onClick={() => onTabChange("endpoints")}
        >
          Endpoints ({endpoints.length})
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
          {endpoints.map((ep) => (
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
          {endpoints.length === 0 && !busy && (
            <li className="text-xs text-muted-foreground py-2 text-center">
              No endpoints yet — open a project to index it.
            </li>
          )}
        </ul>
      )}

      {activeTab === "files" && (
        <div className="flex-1 overflow-hidden">
          <FileTree entries={files} selectedPath={selectedFile} onSelectFile={onSelectFile} />
        </div>
      )}
    </div>
  );
}
