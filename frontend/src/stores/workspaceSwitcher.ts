import { useEffect, useState } from "react";
import { create } from "zustand";
import { persist } from "zustand/middleware";

import { uiStateStorage } from "@/bridge/uistate";
import { listWorkspaceServices, type WorkspaceService } from "@/bridge/workspace";

type WorkspaceSwitcherState = {
  /**
   * The workspace this window is showing in the title bar. UI-only: Sandbox
   * still has no concept of it (workspace.mdx's Registry section is still
   * design-only), but Code Graph's and Git's sidebars read it via
   * useCurrentWorkspaceServices below — the folder they open now comes from
   * here, not a free-standing picker of their own.
   */
  currentId: string | null;
  setCurrent: (id: string) => void;
};

export const useWorkspaceSwitcherStore = create<WorkspaceSwitcherState>()(
  persist(
    (set) => ({
      currentId: null,
      setCurrent: (id) => set({ currentId: id }),
    }),
    {
      name: "workspaceSwitcher",
      storage: uiStateStorage,
      partialize: (state) => ({ currentId: state.currentId }),
    },
  ),
);

/**
 * The current workspace's services — what Code Graph's and Git's project
 * switchers list instead of an arbitrary "open a folder" picker. Re-fetches
 * whenever the current workspace changes; `loaded` starts false so callers
 * can tell "no workspace yet" apart from "still asking Go."
 */
export function useCurrentWorkspaceServices(): { services: WorkspaceService[]; loaded: boolean } {
  const workspaceId = useWorkspaceSwitcherStore((s) => s.currentId);
  const [services, setServices] = useState<WorkspaceService[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    if (!workspaceId) {
      setServices([]);
      setLoaded(true);
      return;
    }
    let cancelled = false;
    setLoaded(false);
    listWorkspaceServices(workspaceId)
      .then((list) => {
        if (!cancelled) setServices(list);
      })
      .catch(() => {
        if (!cancelled) setServices([]);
      })
      .finally(() => {
        if (!cancelled) setLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, [workspaceId]);

  return { services, loaded };
}
