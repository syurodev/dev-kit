import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

type SSHState = {
  selectedId: string;
  openIds: string[];
  favorites: string[];
  select: (id: string) => void;
  close: (id: string) => void;
  favorite: (id: string) => void;
};

// Connection navigation contains no credentials; terminal lifetimes belong to
// the shell store and survive this module being unmounted.
export const useSSHStore = create<SSHState>()(
  persist(
    (set) => ({
      selectedId: "",
      openIds: [],
      favorites: [],
      select: (id) =>
        set((s) => ({
          selectedId: id,
          openIds: s.openIds.includes(id) ? s.openIds : [...s.openIds, id],
        })),
      close: (id) =>
        set((s) => {
          const remaining = s.openIds.filter((key) => key !== id);
          return {
            openIds: remaining,
            selectedId:
              s.selectedId === id
                ? (remaining[remaining.length - 1] ?? "")
                : s.selectedId,
          };
        }),
      favorite: (id) =>
        set((s) => ({
          favorites: s.favorites.includes(id)
            ? s.favorites.filter((key) => key !== id)
            : [...s.favorites, id],
        })),
    }),
    {
      name: "ssh-navigation",
      storage: uiStateStorage,
      partialize: (s) => ({ favorites: s.favorites }),
    },
  ),
);
