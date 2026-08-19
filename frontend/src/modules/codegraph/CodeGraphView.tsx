import { useState } from "react";
import { Dialogs } from "@wailsio/runtime";
import {
  openCodeGraphProject,
  listCodeGraphEndpoints,
  getCodeGraphApiFlow,
  type CodeGraphEndpoint,
  type CodeGraphApiFlow,
} from "../../bridge/codegraph";
import { errorMessage } from "../../bridge/errors";
import { Button } from "../../components/ui/button";

export function CodeGraphView() {
  const [projectPath, setProjectPath] = useState("");
  const [endpoints, setEndpoints] = useState<CodeGraphEndpoint[]>([]);
  const [selectedFlow, setSelectedFlow] = useState<CodeGraphApiFlow | null>(null);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function pickAndOpenProject() {
    setError("");
    let chosen: string;
    try {
      chosen = (await Dialogs.OpenFile({
        CanChooseDirectories: true,
        CanChooseFiles: false,
        Title: "Open Java project",
      })) as string;
    } catch (e) {
      setError(errorMessage(e));
      return;
    }
    if (!chosen) {
      return; // user cancelled the picker
    }
    setProjectPath(chosen);
    await reindex(chosen);
  }

  async function reindex(path: string) {
    setBusy(true);
    setStatus("Indexing…");
    setError("");
    setSelectedFlow(null);
    try {
      await openCodeGraphProject(path);
      const list = await listCodeGraphEndpoints();
      setEndpoints(list);
      setStatus(`Indexed. ${list.length} endpoint(s) found.`);
    } catch (e) {
      setEndpoints([]);
      setStatus("");
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function viewFlow(endpoint: CodeGraphEndpoint) {
    setError("");
    try {
      const flow = await getCodeGraphApiFlow(endpoint.id);
      setSelectedFlow(flow);
    } catch (e) {
      setSelectedFlow(null);
      setError(errorMessage(e));
    }
  }

  return (
    <div className="flex h-full flex-col gap-4 p-4">
      <div className="flex items-center gap-2">
        <Button onClick={pickAndOpenProject} disabled={busy} size="sm">
          Open Java project…
        </Button>
        {projectPath && (
          <Button onClick={() => reindex(projectPath)} disabled={busy} variant="secondary" size="sm">
            Re-index
          </Button>
        )}
        {projectPath && <span className="text-sm text-muted-foreground truncate">{projectPath}</span>}
      </div>

      {status && <p className="text-sm text-muted-foreground">{status}</p>}
      {error && <p className="text-sm text-destructive">{error}</p>}

      <div className="flex flex-1 gap-4 overflow-hidden">
        <ul className="w-1/3 overflow-y-auto border-r pr-2 space-y-1">
          {endpoints.map((ep) => (
            <li key={ep.id}>
              <button
                className="w-full text-left rounded px-2 py-1 hover:bg-surface-raised transition-colors"
                onClick={() => viewFlow(ep)}
              >
                <span className="font-mono text-xs font-semibold uppercase text-status-info mr-1.5">{ep.httpMethod}</span>
                <span className="text-sm">{ep.path}</span>
              </button>
            </li>
          ))}
          {endpoints.length === 0 && !busy && (
            <li className="text-sm text-muted-foreground py-2">No endpoints yet — open a project to index it.</li>
          )}
        </ul>

        <div className="flex-1 overflow-y-auto">
          {selectedFlow ? (
            <ol className="list-decimal pl-5 space-y-2">
              {selectedFlow.steps.map((step, i) => (
                <li key={i} className="font-mono text-sm">
                  <span className="font-medium text-foreground">{step.label}</span>{" "}
                  <span className="text-muted-foreground">({step.filePath}:{step.line})</span>
                </li>
              ))}
            </ol>
          ) : (
            <p className="text-sm text-muted-foreground">Select an endpoint to see its flow.</p>
          )}
        </div>
      </div>
    </div>
  );
}
