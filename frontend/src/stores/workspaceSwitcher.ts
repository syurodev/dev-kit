import { useEffect, useState } from "react";
import { create } from "zustand";
import { persist } from "zustand/middleware";

import { uiStateStorage } from "@/bridge/uistate";
import {
  listWorkspaces,
  listWorkspaceServices,
  onServiceIndexStatus,
  workspaceIndexStatus,
  type ServiceIndexStatus,
  type Workspace,
  type WorkspaceService,
} from "@/bridge/workspace";

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
  /**
   * Bumped when a workspace's folders are edited, so every view listing the
   * current workspace's services reads them again. Not persisted.
   */
  servicesVersion: number;
  servicesChanged: () => void;
};

export const useWorkspaceSwitcherStore = create<WorkspaceSwitcherState>()(
  persist(
    (set) => ({
      currentId: null,
      setCurrent: (id) => set({ currentId: id }),
      servicesVersion: 0,
      servicesChanged: () => set((state) => ({ servicesVersion: state.servicesVersion + 1 })),
    }),
    {
      name: "workspaceSwitcher",
      storage: uiStateStorage,
      partialize: (state) => ({ currentId: state.currentId }),
    },
  ),
);

/**
 * A workspace and its services, by id — the general form behind
 * `useCurrentWorkspaceServices` below. Split out for a caller that needs a
 * SPECIFIC workspace rather than whichever one the title-bar switcher is
 * showing: `BoardVerifyDialog` lists the services of the board's own
 * `workspaceId`, which is not necessarily the one on screen when the dialog
 * opens. Re-fetches whenever `workspaceId` (or the switcher's services
 * version, for edits made to the current workspace's folders) changes;
 * `loaded` starts false so callers can tell "no workspace yet" apart from
 * "still asking Go."
 */
export function useWorkspaceServices(workspaceId: string | null): {
  workspace: Workspace | null;
  services: WorkspaceService[];
  loaded: boolean;
} {
  const servicesVersion = useWorkspaceSwitcherStore((s) => s.servicesVersion);
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [services, setServices] = useState<WorkspaceService[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    if (!workspaceId) {
      setWorkspace(null);
      setServices([]);
      setLoaded(true);
      return;
    }
    let cancelled = false;
    setLoaded(false);
    // listWorkspaces() rather than a get-by-id bridge method: there isn't
    // one, and the list is small (this machine's own registry).
    Promise.all([listWorkspaces(), listWorkspaceServices(workspaceId)])
      .then(([workspaces, list]) => {
        if (cancelled) return;
        setWorkspace(workspaces.find((w) => w.id === workspaceId) ?? null);
        setServices(list);
      })
      .catch(() => {
        if (!cancelled) {
          setWorkspace(null);
          setServices([]);
        }
      })
      .finally(() => {
        if (!cancelled) setLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, [workspaceId, servicesVersion]);

  return { workspace, services, loaded };
}

/**
 * The current workspace and its services — what Code Graph's and Git's
 * project switchers list instead of an arbitrary "open a folder" picker, and
 * what Agent Hub's draft picker offers as "grant every service" (workspace
 * name included, for that item's label).
 */
export function useCurrentWorkspaceServices(): {
  workspace: Workspace | null;
  services: WorkspaceService[];
  loaded: boolean;
} {
  const currentId = useWorkspaceSwitcherStore((s) => s.currentId);
  return useWorkspaceServices(currentId);
}

/**
 * Each service's index state for the current workspace, keyed by path: read
 * once, then kept current by codegraph:indexStatus events. An empty map
 * until the first read answers — callers show nothing rather than a guess.
 */
export function useWorkspaceIndexStatus(): Record<string, ServiceIndexStatus> {
  const workspaceId = useWorkspaceSwitcherStore((s) => s.currentId);
  const servicesVersion = useWorkspaceSwitcherStore((s) => s.servicesVersion);
  const [byPath, setByPath] = useState<Record<string, ServiceIndexStatus>>({});

  useEffect(() => {
    setByPath({});
    if (!workspaceId) return;
    let cancelled = false;
    // Subscribed before the read, so a transition between the two is not
    // lost; the read then only fills paths no event has spoken for.
    const off = onServiceIndexStatus((status) => {
      if (!cancelled) setByPath((prev) => ({ ...prev, [status.path]: status }));
    });
    workspaceIndexStatus(workspaceId)
      .then((list) => {
        if (cancelled) return;
        setByPath((prev) => {
          const next = { ...prev };
          for (const status of list) next[status.path] ??= status;
          return next;
        });
      })
      .catch(() => {});
    return () => {
      cancelled = true;
      off();
    };
  }, [workspaceId, servicesVersion]);

  return byPath;
}
