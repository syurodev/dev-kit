import { useNotesRefreshStore } from "../../stores/notesRefresh";
import { useWorkbenchTabsStore } from "../../stores/workbenchTabs";
import { NotesDashboard } from "./NotesDashboard";

export function NotesView() {
  const refreshToken = useNotesRefreshStore((state) => state.token);
  const bumpNotesRefresh = useNotesRefreshStore((state) => state.bump);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);

  function openNoteTab(note: { id: string; title: string }, preview: boolean) {
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
      onChanged={bumpNotesRefresh}
    />
  );
}
