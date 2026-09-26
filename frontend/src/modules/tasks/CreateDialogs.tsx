import { useEffect, useState, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { errorMessage } from "@/bridge/errors";
import { suggestKeyPrefix } from "./tasksSidebarActions";

/**
 * The two small forms the Tasks rail opens: a board (name + key prefix) and a
 * task (title). A dialog rather than a "New task" row that appears with a
 * placeholder title — the board view does that today, and every one of those
 * is a task named "New task" that somebody has to find and rename.
 *
 * A refused create keeps the dialog open with the reason, the same rule
 * RenameSessionDialog follows: closing first would lose what was typed and
 * turn the error into a toast about a form that is no longer there.
 */
function CreateDialog({
  open,
  title,
  submitLabel,
  canSubmit,
  onSubmit,
  onClose,
  children,
}: {
  open: boolean;
  title: ReactNode;
  submitLabel: string;
  canSubmit: boolean;
  onSubmit: () => Promise<void>;
  onClose: () => void;
  children: ReactNode;
}) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (open) setError("");
  }, [open]);

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <form
          className="space-y-3"
          onSubmit={(event) => {
            event.preventDefault();
            if (!canSubmit || busy) return;
            setBusy(true);
            onSubmit()
              .then(() => {
                setError("");
                onClose();
              })
              .catch((e) => setError(errorMessage(e)))
              .finally(() => setBusy(false));
          }}
        >
          {children}
          {error ? (
            <p role="alert" className="text-caption-11 text-status-danger">
              {error}
            </p>
          ) : null}
          <DialogFooter className="mt-4">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canSubmit || busy}>
              {submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/**
 * `workspaceName` non-null is open. The board is created bound to that
 * workspace in the same call (createBoard's workspaceId), so it is never
 * briefly unlinked — and so never briefly invisible to the agents working
 * there.
 */
export function NewBoardDialog({
  workspaceName,
  onCreate,
  onClose,
}: {
  workspaceName: string | null;
  onCreate: (name: string, keyPrefix: string) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [prefix, setPrefix] = useState("");
  // The prefix follows the name until someone types in it; after that it is
  // theirs, and renaming the board must not overwrite it.
  const [prefixEdited, setPrefixEdited] = useState(false);
  const open = workspaceName !== null;

  useEffect(() => {
    if (!open) return;
    setName("");
    setPrefix("");
    setPrefixEdited(false);
  }, [open]);

  return (
    <CreateDialog
      open={open}
      title={`New board in ${workspaceName ?? ""}`}
      submitLabel="Create"
      canSubmit={!!name.trim() && !!prefix.trim()}
      onSubmit={() => onCreate(name.trim(), prefix.trim())}
      onClose={onClose}
    >
      <div className="space-y-1.5">
        <label className="text-label-12 text-text-primary" htmlFor="new-board-name">
          Name
        </label>
        <Input
          id="new-board-name"
          value={name}
          onChange={(event) => {
            setName(event.target.value);
            if (!prefixEdited) setPrefix(suggestKeyPrefix(event.target.value));
          }}
          placeholder="DevKit"
          maxLength={80}
          autoFocus
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-label-12 text-text-primary" htmlFor="new-board-prefix">
          Key prefix
        </label>
        <Input
          id="new-board-prefix"
          value={prefix}
          onChange={(event) => {
            setPrefix(event.target.value.toUpperCase());
            setPrefixEdited(true);
          }}
          placeholder="DEV"
          maxLength={8}
          spellCheck={false}
          className="font-mono"
        />
        <p className="text-code-11 text-text-tertiary">
          Every task on the board is keyed with it — {prefix.trim() || "DEV"}-3f9a1c.
        </p>
      </div>
    </CreateDialog>
  );
}

/** `boardName` non-null is open. */
export function NewTaskDialog({
  boardName,
  onCreate,
  onClose,
}: {
  boardName: string | null;
  onCreate: (title: string) => Promise<void>;
  onClose: () => void;
}) {
  const [title, setTitle] = useState("");
  const open = boardName !== null;

  useEffect(() => {
    if (open) setTitle("");
  }, [open]);

  return (
    <CreateDialog
      open={open}
      title={`New task on ${boardName ?? ""}`}
      submitLabel="Create"
      canSubmit={!!title.trim()}
      onSubmit={() => onCreate(title.trim())}
      onClose={onClose}
    >
      <Input
        value={title}
        onChange={(event) => setTitle(event.target.value)}
        placeholder="What needs doing"
        aria-label="Task title"
        maxLength={200}
        autoFocus
      />
    </CreateDialog>
  );
}
