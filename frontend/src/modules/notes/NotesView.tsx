import { useNotesRefreshStore } from "../../stores/notesRefresh";
import { getModule } from "../registry";
import { openNoteTab } from "./documents";
import { NotesDashboard } from "./NotesDashboard";

export function NotesView() {
  const refreshToken = useNotesRefreshStore((state) => state.token);
  const bumpNotesRefresh = useNotesRefreshStore((state) => state.bump);

  function preloadEditor() {
    const preload = getModule("notes")?.views.find(
      (view) => view.id === "notes.editor",
    )?.preload?.();
    void preload?.catch(() => undefined);
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
