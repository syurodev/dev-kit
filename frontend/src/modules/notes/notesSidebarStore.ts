import { create } from "zustand";
import type { NoteFolder } from "../../bridge/notes";
import type { NotesScope } from "./NotesSidebar";

export type FolderAction = { kind: "create" } | { kind: "rename" | "tags" | "delete"; folder: NoteFolder };

type NotesSidebarState = {
  scope: NotesScope;
  foldersOpen: boolean;
  folders: NoteFolder[];
  counts: { all: number; ungrouped: number; byFolder: Map<string, number> };
  trashCount: number;
  /** Set by whichever view the click happened in; consumed by the dashboard,
   *  which owns the dialogs. */
  pendingAction: FolderAction | null;

  setScope: (scope: NotesScope) => void;
  setFoldersOpen: (open: boolean) => void;
  setData: (data: {
    folders: NoteFolder[];
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
  foldersOpen: true,
  folders: [],
  counts: { all: 0, ungrouped: 0, byFolder: new Map() },
  trashCount: 0,
  pendingAction: null,

  setScope: (scope) => set({ scope }),
  setFoldersOpen: (foldersOpen) => set({ foldersOpen }),
  setData: (data) => set(data),
  setTrashCount: (trashCount) => set({ trashCount }),
  requestAction: (pendingAction) => set({ pendingAction }),
  clearAction: () => set({ pendingAction: null }),
}));
