import { useCodeGraphStore, codeGraphRequests, type FolderData } from "./codegraphStore";
import type { WorkspaceService } from "@/bridge/workspace";
import { scopeFolders } from "./workspace/scope";

/** Module-level handles every action file shares. */
export const set = useCodeGraphStore.getState().set;
export const snapshot = () => useCodeGraphStore.getState();
export const begin = codeGraphRequests.begin;

export function folderById(folderId: string): WorkspaceService | undefined {
  return snapshot().folders.find((f) => f.id === folderId);
}

const cleanPath = (p: string) => (p.length > 1 ? p.replace(/[\\/]+$/, "") : p);

/** The folder whose registered path is `path` (trailing separators ignored). */
export function folderByPath(path: string): WorkspaceService | undefined {
  const wanted = cleanPath(path);
  return snapshot().folders.find((f) => cleanPath(f.path) === wanted);
}

/** The folders the sidebar currently shows, in workspace order. */
export function foldersInScope(): WorkspaceService[] {
  const { scope, folders } = snapshot();
  return scopeFolders(scope, folders);
}

/**
 * Replaces one folder's data. Other folders' entries keep their identity, so a
 * component subscribed to another folder does not re-render.
 */
export function patchFolder(folderId: string, patch: Partial<FolderData>) {
  const { data } = snapshot();
  const current = data[folderId];
  if (!current) return;
  set({ data: { ...data, [folderId]: { ...current, ...patch } } });
}

export const isCurrentWorkspace = (workspaceId: string) => snapshot().workspaceId === workspaceId;
