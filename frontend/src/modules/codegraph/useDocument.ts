import { useEffect } from "react";
import type { WorkspaceService } from "@/bridge/workspace";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useCodeGraphStore } from "./codegraphStore";
import { ensureStarted, resolveOwner } from "./codegraphActions";
import { MODULE, type ActiveDoc } from "./documents";
import { folderLabels } from "./workspace/identity";

/**
 * Whether this document is the Code Graph tab in front. Its context pane —
 * the inspector — shows only then: two flows side by side must not both
 * claim the one inspector.
 */
export function useIsFrontDocument(tabId: string): boolean {
  return useWorkbenchTabsStore((s) => moduleTabs(s.modules, MODULE).activeTabId === tabId);
}

/** Starts the module from whichever surface mounts first. */
export function useStarted(): void {
  useEffect(ensureStarted, []);
}

/**
 * Who a document belongs to, and how that stands now:
 *
 *   - `owner` is null for a tab saved before documents named their owner whose
 *     folder cannot be told (the view says so and does not guess);
 *   - `folder` is the folder in the OPEN workspace, or undefined when the
 *     document belongs to another workspace or to a folder that has left this
 *     one (`relation` says which);
 *   - `ready` is true once the open workspace's folders are known, so a view
 *     does not call a folder missing in the moment before it is loaded.
 */
export type DocumentOwnership = {
  owner: { workspaceId: string; folderId: string } | null;
  folder: WorkspaceService | undefined;
  relation: "current" | "other-workspace" | "removed" | "unknown";
  ready: boolean;
  /** The folder's display label, unique within the workspace where names repeat. */
  label: string;
};

export function useDocumentOwnership(doc: Pick<ActiveDoc, "workspaceId" | "folderId">): DocumentOwnership {
  useStarted();
  const workspaceId = useCodeGraphStore((s) => s.workspaceId);
  const folders = useCodeGraphStore((s) => s.folders);
  const status = useCodeGraphStore((s) => s.foldersStatus);
  const owner = resolveOwner(doc);
  const ready = status === "ready";
  if (!owner) return { owner: null, folder: undefined, relation: "unknown", ready, label: "" };
  if (owner.workspaceId !== workspaceId) {
    return { owner, folder: undefined, relation: "other-workspace", ready, label: "" };
  }
  const folder = folders.find((f) => f.id === owner.folderId);
  const labels = folderLabels(folders.map((f) => ({ id: f.id, name: f.name, path: f.path })));
  return { owner, folder, relation: folder ? "current" : ready ? "removed" : "unknown", ready, label: folder ? (labels[folder.id] ?? folder.name) : "" };
}

/** A folder's display label in the open workspace, or "" if it is not one. */
export function useFolderLabel(folderId: string | undefined): string {
  const folders = useCodeGraphStore((s) => s.folders);
  if (!folderId) return "";
  const labels = folderLabels(folders.map((f) => ({ id: f.id, name: f.name, path: f.path })));
  return labels[folderId] ?? "";
}
