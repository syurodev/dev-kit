import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

type SSHState = {
  favorites: string[];
  showNewConnection: boolean;
  setShowNewConnection: (show: boolean) => void;
  favorite: (id: string) => void;
};

// Workbench owns connection tabs; only favorites persist here. Dialog state is ephemeral.
export const useSSHStore = create<SSHState>()(
  persist(
    (set) => ({
      favorites: [],
      showNewConnection: false,
      setShowNewConnection: (showNewConnection) => set({ showNewConnection }),
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
