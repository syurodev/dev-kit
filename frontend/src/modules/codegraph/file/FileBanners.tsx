import { useState } from "react";
import { FeedbackBanner } from "@/components/ui/feedback";
import { Button } from "@/components/ui/button";
import { saveDraftCopy } from "@/bridge/workspaceCode";
import { errorMessage } from "@/bridge/errors";
import { conflictUnanswered, type FileBuffer } from "../workspace/bufferModel";
import { baseName } from "../workspace/identity";

/**
 * What a buffer tells the person when the world moved under it.
 *
 * Each banner names the situation, says what is safe, and offers the choices —
 * none of them is taken for the person. A dirty buffer never loses its text to
 * the disk, and a conflict is never overwritten by a plain Save.
 */

export function ExternalChangeBanner({
  buffer,
  onCompare,
  onReload,
  onKeepMine,
  onOverwrite,
  onRecreate,
  onClose,
}: {
  buffer: FileBuffer;
  onCompare: () => void;
  onReload: () => void;
  onKeepMine: () => void;
  onOverwrite: () => void;
  onRecreate: () => void;
  onClose: () => void;
}) {
  // Answered with "Keep mine": a quiet line instead of the full banner, with the
  // same choices — the conflict has not gone anywhere.
  if (!conflictUnanswered(buffer)) {
    return (
      <div role="status" className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border-subtle px-3 py-1 text-code-11 text-status-warning">
        <span>{buffer.external === "deleted" ? "Deleted on disk" : "Disk differs"} — Save is held.</span>
        {buffer.external === "deleted" ? (
          <button type="button" className="underline-offset-2 hover:underline" onClick={onRecreate}>Recreate</button>
        ) : (
          <>
            <button type="button" className="underline-offset-2 hover:underline" onClick={onCompare}>Compare</button>
            <button type="button" className="underline-offset-2 hover:underline" onClick={onReload}>Reload</button>
            <button type="button" className="text-status-danger underline-offset-2 hover:underline" onClick={onOverwrite}>Overwrite…</button>
          </>
        )}
      </div>
    );
  }
  if (buffer.external === "deleted") {
    return (
      <FeedbackBanner
        tone="warning"
        title="This file was deleted on disk"
        className="rounded-none border-x-0"
        action={
          <div className="flex shrink-0 flex-wrap gap-1.5">
            {buffer.dirty || buffer.content ? (
              <Button size="xs" variant="outline" className="bg-surface-raised" onClick={onRecreate}>
                Save to recreate
              </Button>
            ) : null}
            <Button size="xs" variant="ghost" onClick={onClose}>
              Close
            </Button>
          </div>
        }
      >
        {buffer.dirty
          ? "Your edits are still here. Nothing is written back until you choose to recreate the file."
          : "What you see is its last content. Nothing is written back unless you save."}
      </FeedbackBanner>
    );
  }
  return (
    <FeedbackBanner
      tone="warning"
      title="This file changed on disk"
      className="rounded-none border-x-0"
      action={
        <div className="flex shrink-0 flex-wrap gap-1.5">
          <Button size="xs" variant="outline" className="bg-surface-raised" onClick={onCompare}>
            Compare
          </Button>
          <Button size="xs" variant="outline" className="bg-surface-raised" onClick={onReload}>
            Reload from disk
          </Button>
          <Button size="xs" variant="ghost" onClick={onKeepMine}>
            Keep mine
          </Button>
          <Button size="xs" variant="ghost" className="text-status-danger" onClick={onOverwrite}>
            Overwrite disk
          </Button>
        </div>
      }
    >
      Your edits are kept. Saving is held until you decide: Keep mine does not change that, so the disk is never
      overwritten by accident.
    </FeedbackBanner>
  );
}

/** The buffer's folder left the workspace; its edits are drafts that only exist here. */
export function OrphanBanner({
  buffer,
  folderLabel,
  onDiscard,
}: {
  buffer: FileBuffer;
  folderLabel: string;
  onDiscard: () => void;
}) {
  const [note, setNote] = useState("");
  const text = buffer.eol === "\r\n" ? buffer.content.replace(/\r?\n/g, "\r\n") : buffer.content;
  return (
    <FeedbackBanner
      tone="error"
      title="This file's folder is no longer in the workspace"
      className="rounded-none border-x-0"
      action={
        <div className="flex shrink-0 flex-wrap gap-1.5">
          <Button
            size="xs"
            variant="outline"
            className="bg-surface-raised"
            onClick={() => void navigator.clipboard?.writeText(text).then(() => setNote("Copied."), () => setNote("Could not copy."))}
          >
            Copy
          </Button>
          <Button
            size="xs"
            variant="outline"
            className="bg-surface-raised"
            onClick={() =>
              void saveDraftCopy(baseName(buffer.path) || "draft.txt", text).then(
                (path) => setNote(path ? `Saved a copy to ${path}.` : ""),
                (e) => setNote(errorMessage(e)),
              )
            }
          >
            Save a copy…
          </Button>
          <Button size="xs" variant="ghost" onClick={onDiscard}>
            {buffer.dirty ? "Discard edits" : "Close"}
          </Button>
        </div>
      }
    >
      {buffer.dirty
        ? `Your unsaved edits to ${buffer.path} are only here${folderLabel ? ` (it was in ${folderLabel})` : ""}. They cannot be saved into the workspace, and will not be moved to another folder with the same name.`
        : `${buffer.path} belongs to a folder that was removed.`}
      {note ? <span className="ml-2 text-text-primary">{note}</span> : null}
    </FeedbackBanner>
  );
}

/** A mixed-ending file, normalized when opened. */
export function MixedEolNote({ eol }: { eol: "\n" | "\r\n" }) {
  return (
    <p className="shrink-0 border-b border-border-subtle px-4 py-1 text-caption-11 text-text-tertiary" role="status">
      This file mixed line endings. It is shown with {eol === "\r\n" ? "CRLF" : "LF"} and will be saved that way if you edit it.
    </p>
  );
}
