import type { EntityRef, Placement } from "@/workbench/intents";
import type { DocIdentity } from "./workspace/identity";

export type RevealFileArgs = {
  location: DocIdentity;
  line: number | null;
  options: { preview: false; beside: boolean };
};

/**
 * What `codegraph.reveal` opens for a file ref whose caller already resolved
 * it to a workspace folder (`workspaceId` + `folderId`, `id` folder-relative),
 * or null when the ref still needs resolving against a project. Any workspace
 * is fine: `openFile` does not care which one the title bar shows.
 */
export function revealFileArgs(ref: EntityRef, placement: Placement): RevealFileArgs | null {
  if (!ref.workspaceId || !ref.folderId) return null;
  return {
    location: { workspaceId: ref.workspaceId, folderId: ref.folderId, path: ref.id },
    line: ref.line ?? null,
    options: { preview: false, beside: placement === "beside" },
  };
}
