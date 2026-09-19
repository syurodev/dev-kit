import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

export type RecentProject = { path: string; openedAt: number };

const MAX_RECENT = 8;

type RecentProjectsState = {
  projects: RecentProject[];
  remember: (path: string) => void;
  forget: (path: string) => void;
};

/**
 * Projects opened in Code Graph, most recent first — for the project switcher
 * and the "No project open" state. Go remembers only the one active project,
 * so the list is the frontend's, kept in ui_state.
 */
export const useRecentProjectsStore = create<RecentProjectsState>()(
  persist(
    (set) => ({
      projects: [],
      remember: (path) =>
        set((s) => ({
          projects: [{ path, openedAt: Date.now() }, ...s.projects.filter((p) => p.path !== path)].slice(0, MAX_RECENT),
        })),
      forget: (path) => set((s) => ({ projects: s.projects.filter((p) => p.path !== path) })),
    }),
    { name: "codegraph.recentProjects", storage: uiStateStorage, partialize: (s) => ({ projects: s.projects }) },
  ),
);

export const rememberProject = (path: string) => useRecentProjectsStore.getState().remember(path);
