import { useCallback, useMemo, useRef } from "react";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { NotesSidebar } from "./NotesSidebar";
import { useNotesSidebarStore } from "./notesSidebarStore";

/**
 * Registers the Notes rail from whichever view is mounted.
 *
 * Called by both the dashboard and the editor: the rail belongs to the module,
 * not to one of its views, and registering from only one of them leaves it
 * empty as soon as the other is on screen.
 *
 * `onNavigate` lets the editor send the user back to the dashboard when they
 * pick a scope or a folder action — the dialogs live there.
 */
export function useNotesSidebar(onNavigate?: () => void) {
  const scope = useNotesSidebarStore((s) => s.scope);
  const folders = useNotesSidebarStore((s) => s.folders);
  const counts = useNotesSidebarStore((s) => s.counts);
  const trashCount = useNotesSidebarStore((s) => s.trashCount);
  const setScope = useNotesSidebarStore((s) => s.setScope);
  const requestAction = useNotesSidebarStore((s) => s.requestAction);

  // Callers pass a plain function redefined every render. Holding it in a ref
  // keeps it out of the memo's dependencies, so the rail re-registers when what
  // it shows changes and not on every keystroke in the view around it.
  const navigateRef = useRef(onNavigate);
  navigateRef.current = onNavigate;
  const navigate = useCallback(() => navigateRef.current?.(), []);

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
        counts={counts}
        trashCount={trashCount}
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
    [scope, folders, counts, trashCount, setScope, requestAction, navigate],
  );

  useSidebarContent(content);
}
