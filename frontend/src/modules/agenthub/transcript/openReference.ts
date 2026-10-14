import { toast } from "sonner";
import { errorMessage } from "@/bridge/errors";
import { queryClient } from "@/lib/query";
import { dispatchIntent, type EntityRef, type Placement } from "@/workbench/intents";
import type { FileRef } from "./fileRef";
import { fileRefQuery, withLine, type RefContext, type RefResolution, type RefTarget } from "./resolveFileRef";
import { pickTarget } from "./RefPicker";

export type OpenDeps = {
  resolve: (ref: FileRef, ctx: RefContext) => Promise<RefResolution>;
  dispatch: typeof dispatchIntent;
  toastError: (message: string) => void;
  /** Asks which of several files was meant; null when the person dismissed it. */
  pick: (anchor: HTMLElement | null, targets: RefTarget[]) => Promise<RefTarget | null>;
};

export const liveOpenDeps: OpenDeps = {
  // The hover preview and the click share one cached, path-only resolution.
  resolve: async (ref, ctx) => withLine(await queryClient.fetchQuery(fileRefQuery(ref, ctx)), ref.line),
  dispatch: dispatchIntent,
  toastError: (message) => {
    toast.error(message);
  },
  pick: pickTarget,
};

const baseName = (path: string) => path.slice(path.lastIndexOf("/") + 1);

/**
 * Opens a file an agent pointed at, in the session's own workspace: found opens
 * it by folder id, several candidates ask which, none says so, and a session
 * with no workspace falls back to resolving against its project. Placement
 * defaults to beside, so the session stays on screen.
 */
export async function openFileRef(
  ref: FileRef,
  ctx: RefContext | null,
  opts: { placement?: Placement; anchor?: HTMLElement | null },
  deps: OpenDeps = liveOpenDeps,
): Promise<void> {
  const placement = opts.placement ?? "beside";
  // The line is always the reference's own: a cached resolution may have been
  // made for the same path at another line.
  const line = ref.line !== undefined ? { line: ref.line } : {};
  const open = (target: RefTarget) =>
    deps.dispatch(
      "codegraph.reveal",
      {
        module: "codegraph",
        kind: "file",
        id: target.rel,
        label: baseName(target.rel),
        workspaceId: target.workspaceId,
        folderId: target.folderId,
        ...line,
      },
      { placement },
    );
  const byProject = () => {
    const legacy: EntityRef = {
      module: "codegraph",
      kind: "file",
      id: ref.path,
      label: baseName(ref.path),
      project: ctx?.projectPath ?? "",
      ...line,
    };
    return deps.dispatch("codegraph.reveal", legacy, { placement });
  };

  if (!ctx) {
    await byProject();
    return;
  }
  let resolution: RefResolution;
  try {
    resolution = await deps.resolve(ref, ctx);
  } catch (error) {
    deps.toastError(`Couldn't open ${ref.path}: ${errorMessage(error)}`);
    return;
  }
  switch (resolution.kind) {
    case "found":
      await open(resolution.target);
      return;
    case "ambiguous": {
      const picked = await deps.pick(opts.anchor ?? null, resolution.targets);
      if (picked) await open(picked);
      return;
    }
    case "missing":
      deps.toastError(`Couldn't find ${ref.path} in this workspace`);
      return;
    case "unsupported":
      await byProject();
      return;
  }
}

/** What a session's references resolve against; null when there is no session (a draft). */
export function refContextOf(session: { id: string; workspaceId?: string; projectPath: string } | null | undefined): RefContext | null {
  return session ? { sessionId: session.id, workspaceId: session.workspaceId ?? "", projectPath: session.projectPath } : null;
}
