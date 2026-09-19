import { useCallback, useMemo, useRef, type ReactNode } from "react";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { openNoteTab } from "./documents";
import { NotesSidebar } from "./NotesSidebar";
import { useNotesSidebarStore } from "./notesSidebarStore";
import { useNotesRefreshStore } from "../../stores/notesRefresh";
import { deleteNote } from "../../bridge/notes";

/**
 * The Notes rail, as a node.
 *
 * Split from the registration below so the module can draw the same rail with
 * NO view mounted. Views used to be the only source, which held while the
 * workbench always had one open; it stopped holding when tabs became something
 * remembered rather than invented, and closing the last note left the rail
 * blank. NotesModuleSidebar is the floor, the views still register over it.
 *
 * `onNavigate` lets the editor send the user back to the dashboard when they
 * pick a scope or a folder action — the dialogs live there.
 */
export function useNotesSidebarContent(onNavigate?: () => void): ReactNode {
  const scope = useNotesSidebarStore((s) => s.scope);
  const folders = useNotesSidebarStore((s) => s.folders);
  const notes = useNotesSidebarStore((s) => s.notes);
  const counts = useNotesSidebarStore((s) => s.counts);
  const trashCount = useNotesSidebarStore((s) => s.trashCount);
  const expanded = useNotesSidebarStore((s) => s.expanded);
  const setScope = useNotesSidebarStore((s) => s.setScope);
  const toggleExpanded = useNotesSidebarStore((s) => s.toggleExpanded);
  const requestAction = useNotesSidebarStore((s) => s.requestAction);
  const bumpNotesRefresh = useNotesRefreshStore((s) => s.bump);

  // Callers pass a plain function redefined every render. Holding it in a ref
  // keeps it out of the memo's dependencies, so the rail re-registers when what
  // it shows changes and not on every keystroke in the view around it.
  const navigateRef = useRef(onNavigate);
  navigateRef.current = onNavigate;
  const navigate = useCallback(() => navigateRef.current?.(), []);

  // A note row in the rail opens the note the same way a dashboard card does:
  // as the module's preview tab. The rail is the module's, so it may open
  // tabs directly rather than round-tripping through the dashboard.
  const openNote = useCallback((note: { id: string; title: string }, preview = true) => {
    openNoteTab(note, preview);
  }, []);
  const openNoteInTab = useCallback(
    (note: { id: string; title: string }) => openNote(note, false),
    [openNote],
  );
  // Trash is soft (30 days, see NotesTrash), so the rail may do it without a
  // confirm; the dashboard and open editors re-read on the bump.
  const trashNote = useCallback(
    (note: { id: string }) => {
      void deleteNote(note.id).then(bumpNotesRefresh).catch(() => undefined);
    },
    [bumpNotesRefresh],
  );

  // Memoised on what it actually renders. useSidebarContent re-registers
  // whenever the element identity changes, and a fresh element every render
  // would churn the store on every keystroke elsewhere in the view.
  const content = useMemo(
    () => (
      <NotesSidebar
        scope={scope}
        onScopeChange={(next) => {
          setScope(next);
          navigate();
        }}
        folders={folders}
        notes={notes}
        counts={counts}
        trashCount={trashCount}
        expanded={expanded}
        onToggleExpanded={toggleExpanded}
        onNewNote={() => {
          requestAction({ kind: "newNote" });
          navigate();
        }}
        onOpenNote={openNote}
        onOpenNoteInTab={openNoteInTab}
        onTrashNote={trashNote}
        onCreateFolder={() => {
          requestAction({ kind: "create" });
          navigate();
        }}
        onRenameFolder={(folder) => {
          requestAction({ kind: "rename", folder });
          navigate();
        }}
        onEditFolderTags={(folder) => {
          requestAction({ kind: "tags", folder });
          navigate();
        }}
        onDeleteFolder={(folder) => {
          requestAction({ kind: "delete", folder });
          navigate();
        }}
      />
    ),
    [
      scope,
      folders,
      notes,
      counts,
      trashCount,
      expanded,
      setScope,
      toggleExpanded,
      requestAction,
      navigate,
      openNote,
      openNoteInTab,
      trashNote,
    ],
  );

  return content;
}

/**
 * Registers the rail from whichever view is mounted.
 *
 * Called by both the dashboard and the editor: registering from only one of
 * them leaves it empty as soon as the other is on screen.
 */
export function useNotesSidebar(onNavigate?: () => void) {
  useSidebarContent(useNotesSidebarContent(onNavigate));
}

/** The module's own rail, for ModuleManifest.sidebar — see ModuleSidebar. */
export function NotesModuleSidebar() {
  return useNotesSidebarContent();
}
