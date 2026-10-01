import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { listWorkspaceServices, listWorkspaces } from "@/bridge/workspace";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";
import type { SidebarWorkspace } from "./surfaces/AgentSidebarTree";

const NONE: SidebarWorkspace[] = [];

/**
 * Every workspace with its folders, primary first (the backend lists the
 * primary first), for the rail to group sessions by. Re-read when a
 * workspace's services change (servicesVersion).
 */
async function loadSidebarWorkspaces(): Promise<SidebarWorkspace[]> {
  const workspaces = await listWorkspaces();
  return Promise.all(
    workspaces.map(async (workspace) => ({
      id: workspace.id,
      name: workspace.name,
      paths: (await listWorkspaceServices(workspace.id)).map((service) => service.path),
    })),
  );
}

/** Keep the active workspace first in the picker. Session grouping uses its stored ID. */
export function currentFirst(workspaces: SidebarWorkspace[], currentId: string | null): SidebarWorkspace[] {
  const index = currentId ? workspaces.findIndex((w) => w.id === currentId) : -1;
  if (index <= 0) return workspaces;
  return [workspaces[index], ...workspaces.slice(0, index), ...workspaces.slice(index + 1)];
}

export function useSidebarWorkspaces(): SidebarWorkspace[] {
  const currentId = useWorkspaceSwitcherStore((s) => s.currentId);
  const version = useWorkspaceSwitcherStore((s) => s.servicesVersion);
  const query = useQuery({ queryKey: ["agenthub", "sidebarWorkspaces", version], queryFn: loadSidebarWorkspaces });
  const data = query.data ?? NONE;
  return useMemo(() => currentFirst(data, currentId), [data, currentId]);
}
