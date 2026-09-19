import { useWorkbenchTabsStore } from "../../stores/workbenchTabs";
import { getModule } from "../registry";

/**
 * Opens a note as a tab — the dashboard's card click, the rail's row click,
 * and the palette all go through here.
 *
 * Was two copies (NotesView.tsx and useNotesSidebar.tsx), identical apart from
 * their default for `preview`. Neither is a hook: `getModule` and
 * `useWorkbenchTabsStore.getState()` are plain calls, so this also works from
 * a module-level function with no component around it — which the palette
 * search needs, since a note may be opened before Notes has ever mounted.
 */
export function openNoteTab(note: { id: string; title: string }, preview = true): void {
  const preload = getModule("notes")?.views.find((view) => view.id === "notes.editor")?.preload?.();
  void preload?.catch(() => undefined);
  useWorkbenchTabsStore.getState().openTab({
    moduleId: "notes",
    viewId: "notes.editor",
    title: note.title || "Untitled",
    params: { noteId: note.id },
    preview,
  });
}
