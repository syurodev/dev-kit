import { useNotesRefreshStore } from "../../stores/notesRefresh";
import { useWorkbenchTabsStore } from "../../stores/workbenchTabs";
import { getModule } from "../registry";
import { NotesDashboard } from "./NotesDashboard";

export function NotesView() {
  const refreshToken = useNotesRefreshStore((state) => state.token);
  const bumpNotesRefresh = useNotesRefreshStore((state) => state.bump);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);

  function preloadEditor() {
    const preload = getModule("notes")?.views.find(
      (view) => view.id === "notes.editor",
    )?.preload?.();
    void preload?.catch(() => undefined);
  }

  function openNoteTab(note: { id: string; title: string }, preview: boolean) {
    preloadEditor();
    openTab({
      moduleId: "notes",
      viewId: "notes.editor",
      title: note.title || "Untitled",
      params: { noteId: note.id },
      preview,
    });
  }

  return (
    <NotesDashboard
      refreshToken={refreshToken}
      onOpenNote={(note) => openNoteTab(note, true)}
      onOpenNoteDouble={(note) => openNoteTab(note, false)}
      onPreloadEditor={preloadEditor}
      onChanged={bumpNotesRefresh}
    />
  );
}
