import { Events } from "@wailsio/runtime";
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

/**
 * Where a workspace service's index stands (Go codegraph.ServiceIndexStatus,
 * docs/dev-kit/workspace.mdx § "Trạng thái index của một service"). "" means
 * nothing is queued and nothing is on disk.
 */
export type ServiceIndexState = "" | "pending" | "indexing" | "indexed" | "failed" | "unsupported";

export type ServiceIndexStatus = {
  path: string;
  state: ServiceIndexState;
  /** Why, for failed and unsupported. */
  error: string;
  /** The failure is a missing analyzer toolchain, not the code. */
  missingToolchain: boolean;
  /** Unix ms, for indexed; 0 when unknown. */
  indexedAt: number;
};

function toStatus(raw: {
  path?: string;
  state?: string;
  error?: string;
  missingToolchain?: boolean;
  indexedAt?: number;
}): ServiceIndexStatus {
  return {
    path: raw.path ?? "",
    state: (raw.state ?? "") as ServiceIndexState,
    error: raw.error ?? "",
    missingToolchain: raw.missingToolchain ?? false,
    indexedAt: Number(raw.indexedAt ?? 0),
  };
}

export function workspaceIndexStatus(workspaceId: string): Promise<ServiceIndexStatus[]> {
  return callBridge(async () =>
    ((await AppService.WorkspaceIndexStatus(workspaceId)) ?? []).map(toStatus),
  );
}

/** Queue one service to be indexed again — Retry. Returns at once. */
export function reindexWorkspaceService(path: string): Promise<void> {
  return callBridge(() => AppService.WorkspaceReindexService(path) as Promise<void>);
}

/** Every index-queue transition: queued, running, and how it ended. */
export function onServiceIndexStatus(handler: (status: ServiceIndexStatus) => void): () => void {
  return Events.On("codegraph:indexStatus", (ev) => handler(toStatus(ev.data ?? {})));
}
