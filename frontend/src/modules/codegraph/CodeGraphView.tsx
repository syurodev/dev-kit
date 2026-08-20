import { useMemo, useRef, useState } from "react";
import { Dialogs } from "@wailsio/runtime";
import { keymap } from "@codemirror/view";
import type { Extension } from "@codemirror/state";
import { java } from "@codemirror/lang-java";
import { json } from "@codemirror/lang-json";
import { yaml } from "@codemirror/lang-yaml";
import { markdown } from "@codemirror/lang-markdown";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { CodeGraphSidebar } from "./CodeGraphSidebar";
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

  useSidebarContent(
    <CodeGraphSidebar
      projectPath={projectPath}
      busy={busy}
      status={status}
      error={error}
      activeTab={activeTab}
      onTabChange={setActiveTab}
      onPickProject={pickAndOpenProject}
      onReindex={() => reindex(projectPath)}
      endpoints={endpoints}
      selectedEndpointId={rootId}
      onSelectEndpoint={viewFlow}
      files={files}
      selectedFile={selectedFile}
      onSelectFile={openFile}
    />,
  );

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      {activeTab === "endpoints" && (
        <div className="flex flex-1 flex-col overflow-hidden">
          {subgraph && rootId ? (
            <div className="flex flex-1 flex-col overflow-hidden">
              <span className="px-3 pt-2 font-mono text-[11px] text-text-tertiary">
                {subgraph.nodes.length} nodes · {subgraph.edges.length} edges
              </span>
              <div className="flex-1 overflow-hidden">
                <FlowCanvas
                  key={rootId}
                  rootId={rootId}
                  nodes={subgraph.nodes}
                  edges={subgraph.edges}
                />
              </div>
            </div>
          ) : (
            <div className="flex h-full items-center justify-center p-4 text-sm text-muted-foreground">
              Select an endpoint from the sidebar to view its flow graph.
            </div>
          )}
        </div>
      )}

      {activeTab === "files" && (
        <div className="flex flex-1 flex-col overflow-hidden">
          {selectedFile ? (
            <div className="flex flex-1 flex-col overflow-hidden">
              <div className="flex items-center justify-between border-b px-3 py-1.5 text-sm text-muted-foreground">
                <span className="truncate font-mono">{selectedFile}</span>
                {fileDirty && <span className="text-status-warning text-xs font-mono">unsaved</span>}
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
            </div>
          ) : (
            <div className="flex h-full items-center justify-center p-4 text-sm text-muted-foreground">
              Select a file from the sidebar to edit it. Ctrl/Cmd+S saves.
            </div>
          )}
          {fileError && <p className="px-3 py-1 text-xs text-destructive">{fileError}</p>}
        </div>
      )}
    </div>
  );
}
