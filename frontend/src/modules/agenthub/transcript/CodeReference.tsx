import * as React from "react";
import { Copy, GitCommitHorizontal, SquareArrowOutUpRight } from "lucide-react";
import { toast } from "sonner";

import { errorMessage } from "@/bridge/errors";
import { pinWorkFolder } from "@/bridge/workfolder";
import { Button } from "@/components/ui/button";
import { dispatchIntent, type Placement } from "@/workbench/intents";
import { projectFolderPath, type SymbolHit } from "./codeRefs";
import { openRefTarget } from "./openReference";
import {
  copyWithNotice,
  FilePreviewView,
  filePreviewState,
  PreviewFooter,
  ReferenceMenu,
  ReferencePreview,
  useRefTargetRead,
} from "./ReferencePreview";
import { commitMenuItems, symbolMenuItems, type CommitMenuId, type SymbolMenuId } from "./referenceMenu";
import type { RefContext, RefTarget } from "./resolveFileRef";
import { useTranscriptActions } from "./transcriptActions";
import { workspaceFolders } from "./useCodeRefs";

const SHORT_SHA = 7;
const baseName = (path: string) => path.replace(/\/+$/, "").slice(path.replace(/\/+$/, "").lastIndexOf("/") + 1);

function symbolTarget(hit: SymbolHit): RefTarget {
  return { workspaceId: hit.workspaceId, folderId: hit.folderId, folderName: hit.folderName, rel: hit.filePath, line: hit.line };
}

/** Opens a symbol at its definition by folder id, and says so when it cannot. */
export async function openSymbol(hit: SymbolHit, placement: Placement) {
  try {
    if ((await openRefTarget(symbolTarget(hit), hit.line, placement)) === "unavailable") {
      toast.error(`Couldn't find ${hit.filePath} in this workspace`);
    }
  } catch (error) {
    toast.error(`Couldn't open ${hit.label}: ${errorMessage(error)}`);
  }
}

/** The file preview at the definition line; reads only while the card is open. */
function SymbolPreview({ hit, onOpen }: { hit: SymbolHit; onOpen: (placement?: Placement) => void }) {
  const target = symbolTarget(hit);
  const read = useRefTargetRead(target);
  // The symbol is already found: the preview is a file preview with nothing left to resolve.
  const state = filePreviewState({ sessionId: "", workspaceId: hit.workspaceId, projectPath: "" }, { data: { kind: "found", target }, error: null }, read);
  return <FilePreviewView fileRef={{ path: hit.filePath, line: hit.line }} state={state} kind={hit.kind} onOpen={onOpen} onPick={() => {}} />;
}

const SYMBOL_ICONS: Record<SymbolMenuId, React.ReactNode> = { definition: <SquareArrowOutUpRight />, copy: <Copy /> };
const COMMIT_ICONS: Record<CommitMenuId, React.ReactNode> = { open: <GitCommitHorizontal />, copy: <Copy /> };

/** A symbol in a finished message, with the file preview at its definition and its menu. */
export function SymbolReference({ hit, name, children }: { hit: SymbolHit; name: string; children: React.ReactElement }) {
  const { refPlacement } = useTranscriptActions();
  const placement = refPlacement ?? "beside";
  return (
    <ReferencePreview
      preview={({ close }) => (
        <SymbolPreview
          hit={hit}
          onOpen={(chosen) => {
            close();
            // No placement named: where this surface opens things.
            void openSymbol(hit, chosen ?? placement);
          }}
        />
      )}
      menu={() => (
        <ReferenceMenu
          items={symbolMenuItems()}
          icons={SYMBOL_ICONS}
          onSelect={(id) => {
            if (id === "definition") void openSymbol(hit, placement);
            else void copyWithNotice(name.replace(/\(\)$/, ""), "Name copied");
          }}
        />
      )}
    >
      {children}
    </ReferencePreview>
  );
}

export type CommitOpenDeps = {
  folders: (workspaceId: string) => Promise<{ id: string; path: string }[]>;
  pin: (module: "git", path: string) => Promise<void>;
  dispatch: typeof dispatchIntent;
  toastError: (message: string) => void;
};

const liveCommitDeps: CommitOpenDeps = {
  folders: workspaceFolders,
  pin: pinWorkFolder,
  dispatch: dispatchIntent,
  toastError: (message) => {
    toast.error(message);
  },
};

/**
 * Opens a commit in Git, in the session's repository: Git is pinned to the
 * workspace folder holding the project (or the project itself) first, since
 * a hash means nothing in whatever repository Git last had open. Pinning is a
 * side effect, so it happens on click only.
 */
export async function openCommitRef(sha: string, ctx: RefContext, placement: Placement, deps: CommitOpenDeps = liveCommitDeps): Promise<void> {
  const short = sha.slice(0, SHORT_SHA);
  let folder = ctx.projectPath;
  if (ctx.workspaceId && folder) {
    // A folder list that fails still leaves the project to pin.
    folder = projectFolderPath(folder, await deps.folders(ctx.workspaceId).catch(() => []));
  }
  try {
    if (folder) await deps.pin("git", folder);
    const outcome = await deps.dispatch("git.reveal", { module: "git", kind: "commit", id: sha, label: short }, { placement });
    if (outcome === "unavailable") deps.toastError(`Couldn't open ${short}: Git isn't available`);
  } catch (error) {
    deps.toastError(`Couldn't open ${short} in Git: ${errorMessage(error)}`);
  }
}

/** A commit's hover card: what is known without fetching anything. */
export function CommitPreview({ sha, folder, onOpen }: { sha: string; folder: string; onOpen: () => void }) {
  return (
    <div data-testid="ref-preview-commit" className="flex flex-col">
      <div className="flex min-w-0 items-baseline gap-2 p-2.5">
        <span className="shrink-0 text-code-12 font-medium text-text-primary">{sha.slice(0, SHORT_SHA)}</span>
        {folder ? <span className="min-w-0 truncate text-caption-11 text-text-tertiary">{`Commit in ${folder}`}</span> : null}
      </div>
      <PreviewFooter>
        <Button size="xs" variant="secondary" onClick={onOpen}>
          Open in Git
        </Button>
      </PreviewFooter>
    </div>
  );
}

/** A commit hash in a finished message, with its card and menu. */
export function CommitReference({ sha, refContext, children }: { sha: string; refContext: RefContext; children: React.ReactElement }) {
  const { refPlacement } = useTranscriptActions();
  const placement = refPlacement ?? "beside";
  return (
    <ReferencePreview
      preview={({ close }) => (
        <CommitPreview
          sha={sha}
          folder={baseName(refContext.projectPath)}
          onOpen={() => {
            close();
            void openCommitRef(sha, refContext, placement);
          }}
        />
      )}
      menu={() => (
        <ReferenceMenu
          items={commitMenuItems()}
          icons={COMMIT_ICONS}
          onSelect={(id) => {
            if (id === "open") void openCommitRef(sha, refContext, placement);
            else void copyWithNotice(sha, "SHA copied");
          }}
        />
      )}
    >
      {children}
    </ReferencePreview>
  );
}
