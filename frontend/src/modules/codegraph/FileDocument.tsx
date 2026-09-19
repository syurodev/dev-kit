import { useMemo } from "react";
import { FileCode2 } from "lucide-react";
import { keymap } from "@codemirror/view";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { Button } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { BlastRadiusPanel } from "./BlastRadiusPanel";
import { languageExtensionForPath } from "./editorLanguage";
import { useCodeGraphStore } from "./codegraphStore";
import {
  cancelPendingOpen,
  closeFile,
  confirmDiscardAndOpen,
  editFileContent,
  openFile,
  saveCurrentFile,
} from "./codegraphActions";

const set = useCodeGraphStore.getState().set;

const saveKeymap = keymap.of([
  {
    key: "Mod-s",
    run: () => {
      void saveCurrentFile();
      return true;
    },
  },
]);

const onOpenFile = (path: string) => void openFile(path);

async function copyBuffer() {
  try {
    await navigator.clipboard.writeText(useCodeGraphStore.getState().fileContent);
  } catch {
    set({ fileError: "Could not copy to the clipboard — select the text in the editor and copy it by hand." });
  }
}

/** The Files tab: one file open for editing, plus what depends on it. */
export function FilesPane() {
  const projectPath = useCodeGraphStore((s) => s.projectPath);
  const selectedFile = useCodeGraphStore((s) => s.selectedFile);
  const fileRoot = useCodeGraphStore((s) => s.fileRoot);
  const selectedLine = useCodeGraphStore((s) => s.selectedLine);
  const fileContent = useCodeGraphStore((s) => s.fileContent);
  const fileDirty = useCodeGraphStore((s) => s.fileDirty);
  const fileError = useCodeGraphStore((s) => s.fileError);
  const pendingOpenFile = useCodeGraphStore((s) => s.pendingOpenFile);

  // A buffer left over from a project switch (kept only because it had
  // unsaved edits — see switchProject). Saving it is refused here and by Go.
  const orphaned = !!selectedFile && !!fileRoot && fileRoot !== projectPath;

  const editorExtensions = useMemo(
    () => (selectedFile ? [...languageExtensionForPath(selectedFile), saveKeymap] : []),
    [selectedFile],
  );

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {selectedFile ? (
        <div className="flex flex-1 flex-col overflow-hidden">
          <div className="flex h-9 items-center justify-between gap-3 border-b border-border-subtle px-6">
            <span className="truncate text-code-11 text-text-secondary">{selectedFile}</span>
            {fileDirty && <span className="shrink-0 text-code-11 text-status-warning">unsaved</span>}
          </div>
          {orphaned && (
            <div role="alert" className="flex items-center gap-3 border-b border-border-subtle px-6 py-2">
              <p className="min-w-0 flex-1 text-body-12 text-status-warning">
                This file is from {fileRoot}, not the project now open, and cannot be saved here.
                Copy your edits before closing it.
              </p>
              <Button size="xs" variant="outline" className="bg-surface-raised" onClick={() => void copyBuffer()}>
                Copy
              </Button>
              <Button size="xs" variant="ghost" onClick={closeFile}>
                Discard
              </Button>
            </div>
          )}
          <div className="flex-1 overflow-hidden">
            <CodeEditor
              value={fileContent}
              revealLine={selectedLine}
              onChange={editFileContent}
              extensions={editorExtensions}
            />
          </div>
        </div>
      ) : (
        <EmptyState
          icon={FileCode2}
          title="Pick a file to edit"
          body="Select a file from the sidebar to open it here. ⌘S saves it back to disk; Impact lists what else depends on it."
        />
      )}
      {selectedFile && !orphaned && (
        <BlastRadiusPanel key={selectedFile} filePath={selectedFile} onOpenFile={onOpenFile} />
      )}
      {fileError && <p role="alert" className="px-6 py-1 text-body-12 text-status-danger">{fileError}</p>}

      <AlertDialog open={pendingOpenFile !== null} onOpenChange={(open) => !open && cancelPendingOpen()}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Discard unsaved changes?</AlertDialogTitle>
            <AlertDialogDescription>
              {`${selectedFile ?? "This file"} has edits that are not saved. Opening ${pendingOpenFile ?? "another file"} throws them away.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Keep editing</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void confirmDiscardAndOpen()}>
              Discard and open
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
