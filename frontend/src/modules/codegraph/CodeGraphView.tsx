import { useMemo } from "react";
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
} from "../../bridge/codegraph";
import { errorMessage } from "../../bridge/errors";
import { useCodeGraphStore } from "./codegraphStore";

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

// Reads and writes go through the module store rather than useState — see
// codegraphStore.ts for why the view cannot own this state.
const set = useCodeGraphStore.getState().set;
const snapshot = () => useCodeGraphStore.getState();

async function loadFiles() {
  try {
    set({ files: await listCodeGraphFiles() });
  } catch (e) {
    set({ fileError: errorMessage(e) });
  }
}

async function reindex(path: string) {
  set({ busy: true, status: "Indexing…", error: "", subgraph: null, rootId: null });
  try {
    await openCodeGraphProject(path);
    const list = await listCodeGraphEndpoints();
    set({ endpoints: list });
    await loadFiles();
    set({ status: `Indexed. ${list.length} endpoint(s) found.` });
  } catch (e) {
    set({ endpoints: [], files: [], status: "", error: errorMessage(e) });
  } finally {
    set({ busy: false });
  }
}

async function pickAndOpenProject() {
  set({ error: "" });
  let chosen: string;
  try {
    chosen = (await Dialogs.OpenFile({
      CanChooseDirectories: true,
      CanChooseFiles: false,
      Title: "Open Java project",
    })) as string;
  } catch (e) {
    set({ error: errorMessage(e) });
    return;
  }
  if (!chosen) {
    return; // user cancelled the picker
  }
  set({ projectPath: chosen });
  await reindex(chosen);
}

async function viewFlow(endpoint: CodeGraphEndpoint) {
  set({ error: "" });
  try {
    const sub = await getCodeGraphNeighbors(endpoint.entryNode, 3);
    set({ subgraph: sub, rootId: endpoint.entryNode });
  } catch (e) {
    set({ subgraph: null, error: errorMessage(e) });
  }
}

async function openFile(path: string) {
  set({ fileError: "" });
  try {
    const content = await readCodeGraphFile(path);
    set({ selectedFile: path, fileContent: content, fileDirty: false });
  } catch (e) {
    set({ fileError: errorMessage(e) });
  }
}

// Reads the live values off the store rather than through refs mirrored during
// render (fileContentRef.current = fileContent), which the keymap needed back
// when this state was local and which is a render-phase side effect.
async function saveCurrentFile() {
  const { selectedFile, fileContent } = snapshot();
  if (!selectedFile) return;
  try {
    await writeCodeGraphFile(selectedFile, fileContent);
    set({ fileDirty: false });
  } catch (e) {
    set({ fileError: errorMessage(e) });
  }
}

const saveKeymap = keymap.of([
  {
    key: "Mod-s",
    run: () => {
      void saveCurrentFile();
      return true;
    },
  },
]);

export function CodeGraphView() {
  const projectPath = useCodeGraphStore((s) => s.projectPath);
  const endpoints = useCodeGraphStore((s) => s.endpoints);
  const subgraph = useCodeGraphStore((s) => s.subgraph);
  const rootId = useCodeGraphStore((s) => s.rootId);
  const status = useCodeGraphStore((s) => s.status);
  const error = useCodeGraphStore((s) => s.error);
  const busy = useCodeGraphStore((s) => s.busy);
  const activeTab = useCodeGraphStore((s) => s.activeTab);
  const files = useCodeGraphStore((s) => s.files);
  const selectedFile = useCodeGraphStore((s) => s.selectedFile);
  const fileContent = useCodeGraphStore((s) => s.fileContent);
  const fileDirty = useCodeGraphStore((s) => s.fileDirty);
  const fileError = useCodeGraphStore((s) => s.fileError);

  const editorExtensions = useMemo(
    () => (selectedFile ? [...languageExtensionForPath(selectedFile), saveKeymap] : []),
    [selectedFile],
  );

  useSidebarContent(
    <CodeGraphSidebar
      projectPath={projectPath}
      busy={busy}
      status={status}
      error={error}
      activeTab={activeTab}
      onTabChange={(tab) => set({ activeTab: tab })}
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
                  onChange={(v) => set({ fileContent: v, fileDirty: true })}
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
