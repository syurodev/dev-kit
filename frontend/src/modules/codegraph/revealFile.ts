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

export type ProjectOpen = "opened" | "failed" | "not-found";

export type RevealFileActions = {
  openResolvedFile: (args: RevealFileArgs) => Promise<boolean>;
  openFileInProject: (root: string, path: string, line: number | undefined, placement: Placement) => Promise<ProjectOpen>;
};

/**
 * `codegraph.reveal` for a file: by ids when the caller resolved it, else by
 * project. "unavailable" only when no folder holds the file and nothing was
 * opened; a tab that opened and failed to load shows its own error.
 */
export async function revealFileRef(ref: EntityRef, placement: Placement, actions: RevealFileActions): Promise<"ok" | "unavailable"> {
  const direct = revealFileArgs(ref, placement);
  if (direct) {
    await actions.openResolvedFile(direct);
    return "ok";
  }
  const opened = await actions.openFileInProject(ref.project ?? "", ref.id, ref.line, placement);
  return opened === "not-found" ? "unavailable" : "ok";
}
