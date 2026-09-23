import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  Workspace,
  WorkspaceService,
} from "../../bindings/devkit/internal/workspace/models";

export type { Workspace, WorkspaceService };

export function listWorkspaces(): Promise<Workspace[]> {
  return callBridge(async () => (await AppService.WorkspaceList()) ?? []);
}

export function listWorkspaceServices(workspaceId: string): Promise<WorkspaceService[]> {
  return callBridge(
    async () => (await AppService.WorkspaceListServices(workspaceId)) ?? [],
  );
}

/** "" means the picker was cancelled — not an error. */
export function chooseWorkspaceFolder(startIn: string): Promise<string> {
  return callBridge(() => AppService.WorkspaceChooseFolder(startIn));
}

export type AddWorkspaceResult = { workspace: Workspace; services: WorkspaceService[] };

/**
 * name may be "" — the backend falls back to root's own folder name. root
 * gets auto-discovery's split: one service per nested git checkout it finds,
 * or root itself when it finds none (docs/dev-kit/workspace.mdx).
 */
export function addWorkspace(name: string, root: string): Promise<AddWorkspaceResult> {
  return callBridge(async () => {
    const result = await AppService.WorkspaceAdd(name, root);
    return { workspace: result.workspace, services: result.services ?? [] };
  });
}

export function deleteWorkspace(id: string): Promise<void> {
  return callBridge(() => AppService.WorkspaceDelete(id) as Promise<void>);
}
