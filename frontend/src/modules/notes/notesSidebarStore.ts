import { create } from "zustand";
import type { NoteFolder, NoteSummary } from "../../bridge/notes";
import type { NotesScope } from "./NotesSidebar";

export type FolderAction =
  | { kind: "create" }
  | { kind: "newNote" }
  | { kind: "rename" | "tags" | "delete"; folder: NoteFolder };

type NotesSidebarState = {
  scope: NotesScope;
  folders: NoteFolder[];
  /** Every live note, so the rail can list a folder's notes under it when the
   * folder is expanded (Figma 812:3731 shows "Work" open with its notes). */
  notes: NoteSummary[];
  counts: { all: number; ungrouped: number; byFolder: Map<string, number> };
  trashCount: number;
  /** Which expandable rows (folder ids, or "ungrouped") are open. */
  expanded: Set<string>;
  /** Set by whichever view the click happened in; consumed by the dashboard,
   *  which owns the dialogs and note creation. */
  pendingAction: FolderAction | null;

  setScope: (scope: NotesScope) => void;
  toggleExpanded: (id: string) => void;
  setData: (data: {
    folders: NoteFolder[];
    notes: NoteSummary[];
    counts: { all: number; ungrouped: number; byFolder: Map<string, number> };
  }) => void;
  setTrashCount: (count: number) => void;
  requestAction: (action: FolderAction) => void;
  clearAction: () => void;
};

/**
 * The sidebar outlives any one Notes view.
 *
 * useSidebarContent clears a module's rail when the registering component
 * unmounts, and the dashboard and the editor are separate views — so a sidebar
 * registered only by the dashboard vanished the moment a note was opened. Both
 * views register now, and the state they render has to live somewhere neither
 * of them owns.
 */
export const useNotesSidebarStore = create<NotesSidebarState>((set) => ({
  scope: "all",
  folders: [],
  notes: [],
  counts: { all: 0, ungrouped: 0, byFolder: new Map() },
  trashCount: 0,
  expanded: new Set(),
  pendingAction: null,

  setScope: (scope) => set({ scope }),
  toggleExpanded: (id) =>
    set((state) => {
      const expanded = new Set(state.expanded);
      if (expanded.has(id)) {
        expanded.delete(id);
      } else {
        expanded.add(id);
      }
      return { expanded };
    }),
  // Identity-guarded, the same way sidebarContent.ts guards setContent, and for
  // the same reason: the dashboard both writes this store from an effect and
  // reads it back through the rail's selectors in the same component, so a set
  // that changes nothing still re-renders the writer. That is a loop waiting
  // for an unstable caller — which is exactly what a `= []` query default was.
  // The reference check is enough: every caller hands over values it already
  // holds by identity, so equal contents always mean equal references.
  setData: (data) =>
    set((state) =>
      state.folders === data.folders &&
      state.notes === data.notes &&
      state.counts === data.counts
        ? state
        : data,
    ),
  setTrashCount: (trashCount) => set({ trashCount }),
  requestAction: (pendingAction) => set({ pendingAction }),
  clearAction: () => set({ pendingAction: null }),
}));
