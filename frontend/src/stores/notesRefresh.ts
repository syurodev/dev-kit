import { create } from "zustand";

/**
 * NotesDashboard and NoteEditor are now two independently-mounted tabs
 * (not parent/child), so a change in one (save, delete, folder edit) can't
 * just bump a local `refreshToken` prop passed down — both sides share
 * this instead.
 */
type NotesRefreshState = {
  token: number;
  bump: () => void;
};

export const useNotesRefreshStore = create<NotesRefreshState>((set) => ({
  token: 0,
  bump: () => set((state) => ({ token: state.token + 1 })),
}));
