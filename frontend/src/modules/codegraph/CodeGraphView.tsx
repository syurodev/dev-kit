import { useMemo, useRef, useState } from "react";
import { Dialogs } from "@wailsio/runtime";
import { keymap } from "@codemirror/view";
import type { Extension } from "@codemirror/state";
import { java } from "@codemirror/lang-java";
import { json } from "@codemirror/lang-json";
import { yaml } from "@codemirror/lang-yaml";
import { markdown } from "@codemirror/lang-markdown";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { FileTree } from "./FileTree";
import { FlowCanvas } from "./graph/FlowCanvas";
import {
  openCodeGraphProject,
  listCodeGraphEndpoints,
  getCodeGraphNeighbors,
  listCodeGraphFiles,
  readCodeGraphFile,
  writeCodeGraphFile,
  type CodeGraphEndpoint,
  type CodeGraphSubgraph,
  type CodeGraphFileEntry,
} from "../../bridge/codegraph";
import { errorMessage } from "../../bridge/errors";
import { Button } from "../../components/ui/button";

// Mirrors internal/codegraph/service.go's languageForPath exactly (same 4
// extensions, same default-to-none for anything else) — kept in sync by
// inspection since Go and TS can't share this switch directly.
function languageExtensionForPath(path: string): Extension[] {
  const ext = path.split(".").pop()?.toLowerCase();
  switch (ext) {
    case "java":
      return [java()];
    case "json":
      return [json()];
    case "yaml":
    case "yml":
      return [yaml()];
    case "md":
      return [markdown()];
    default:
      return [];
  }
}

export function CodeGraphView() {
  const [projectPath, setProjectPath] = useState("");
  const [endpoints, setEndpoints] = useState<CodeGraphEndpoint[]>([]);
  const [subgraph, setSubgraph] = useState<CodeGraphSubgraph | null>(null);
  const [rootId, setRootId] = useState<string | null>(null);
  const [, setSelectedNodeId] = useState<string | null>(null);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const [activeTab, setActiveTab] = useState<"endpoints" | "files">("endpoints");
  const [files, setFiles] = useState<CodeGraphFileEntry[]>([]);
  const [selectedFile, setSelectedFile] = useState<string | null>(null);
  const [fileContent, setFileContent] = useState("");
  const [fileDirty, setFileDirty] = useState(false);
  const [fileError, setFileError] = useState("");
  const fileContentRef = useRef(fileContent);
  fileContentRef.current = fileContent;
  const selectedFileRef = useRef(selectedFile);
  selectedFileRef.current = selectedFile;

  async function loadFiles() {
    try {
      setFiles(await listCodeGraphFiles());
    } catch (e) {
      setFileError(errorMessage(e));
    }
  }

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
    setSubgraph(null);
    setRootId(null);
    setSelectedNodeId(null);
    try {
      await openCodeGraphProject(path);
      const list = await listCodeGraphEndpoints();
      setEndpoints(list);
      await loadFiles();
      setStatus(`Indexed. ${list.length} endpoint(s) found.`);
    } catch (e) {
      setEndpoints([]);
      setFiles([]);
      setStatus("");
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function viewFlow(endpoint: CodeGraphEndpoint) {
    setError("");
    try {
      const sub = await getCodeGraphNeighbors(endpoint.entryNode, 3);
      setSubgraph(sub);
      setRootId(endpoint.entryNode);
      setSelectedNodeId(endpoint.entryNode);
    } catch (e) {
      setSubgraph(null);
      setError(errorMessage(e));
    }
  }

  async function openFile(path: string) {
    setFileError("");
    try {
      const content = await readCodeGraphFile(path);
      setSelectedFile(path);
      setFileContent(content);
      setFileDirty(false);
    } catch (e) {
      setFileError(errorMessage(e));
    }
  }

  async function saveCurrentFile() {
    const path = selectedFileRef.current;
    if (!path) return;
    try {
      await writeCodeGraphFile(path, fileContentRef.current);
      setFileDirty(false);
    } catch (e) {
      setFileError(errorMessage(e));
    }
  }

  const saveKeymap = useMemo(
    () =>
      keymap.of([
        {
          key: "Mod-s",
          run: () => {
            void saveCurrentFile();
            return true;
          },
        },
      ]),
    [],
  );

  const editorExtensions = useMemo(
    () => (selectedFile ? [...languageExtensionForPath(selectedFile), saveKeymap] : []),
    [selectedFile, saveKeymap],
  );

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

      <div className="flex items-center gap-2 border-b pb-2">
        <Button variant={activeTab === "endpoints" ? "secondary" : "ghost"} size="sm" onClick={() => setActiveTab("endpoints")}>
          Endpoints
        </Button>
        <Button variant={activeTab === "files" ? "secondary" : "ghost"} size="sm" onClick={() => setActiveTab("files")}>
          Files
        </Button>
      </div>

      {activeTab === "endpoints" && (
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

          {subgraph && rootId ? (
            <div className="flex flex-1 flex-col overflow-hidden">
              <span className="px-1 pb-2 font-mono text-[11px] text-text-tertiary">
                {subgraph.nodes.length} nodes · {subgraph.edges.length} edges
              </span>
              <div className="flex-1 overflow-hidden">
                <FlowCanvas
                  rootId={rootId}
                  nodes={subgraph.nodes}
                  edges={subgraph.edges}
                  onSelect={setSelectedNodeId}
                />
              </div>
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">Select an endpoint to see its flow.</p>
          )}
        </div>
      )}

      {activeTab === "files" && (
        <div className="flex flex-1 gap-4 overflow-hidden">
          <div className="w-1/4 overflow-hidden border-r">
            <FileTree entries={files} selectedPath={selectedFile} onSelectFile={openFile} />
          </div>
          <div className="flex flex-1 flex-col overflow-hidden">
            {selectedFile ? (
              <>
                <div className="flex items-center justify-between border-b px-2 py-1 text-sm text-muted-foreground">
                  <span className="truncate font-mono">{selectedFile}</span>
                  {fileDirty && <span className="text-status-warning">unsaved</span>}
                </div>
                <div className="flex-1 overflow-hidden">
                  <CodeEditor
                    value={fileContent}
                    onChange={(v) => {
                      setFileContent(v);
                      setFileDirty(true);
                    }}
                    extensions={editorExtensions}
                  />
                </div>
              </>
            ) : (
              <p className="p-4 text-sm text-muted-foreground">Select a file to edit it. Ctrl/Cmd+S saves.</p>
            )}
          </div>
        </div>
      )}
      {fileError && <p className="text-sm text-destructive">{fileError}</p>}
    </div>
  );
}
