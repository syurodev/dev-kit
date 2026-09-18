import * as React from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { errorMessage as describeError } from "@/bridge/errors";

/** What the rename dialog says after Save: "" once renamed, the reason when it was not. */
export async function submitRename(title: string, rename: (title: string) => void | Promise<void>): Promise<string> {
  try {
    await rename(title);
    return "";
  } catch (error) {
    return describeError(error);
  }
}

/**
 * Renames one session. Opened from the sidebar row's context menu, so it names
 * whichever row was right-clicked — not necessarily the one on screen.
 * `initialTitle` non-null is open; the view clears it to close.
 */
export function RenameSessionDialog({
  initialTitle,
  onRename,
  onClose,
}: {
  initialTitle: string | null;
  onRename: (title: string) => void | Promise<void>;
  onClose: () => void;
}) {
  const [value, setValue] = React.useState("");
  const [error, setError] = React.useState("");
  const open = initialTitle !== null;

  React.useEffect(() => {
    if (initialTitle === null) return;
    setValue(initialTitle);
    setError("");
  }, [initialTitle]);

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Rename session</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            const title = value.trim();
            if (!title) return;
            // A refused rename keeps the dialog open with the reason, so the
            // name can be fixed and saved again.
            void submitRename(title, onRename).then((reason) => {
              setError(reason);
              if (!reason) onClose();
            });
          }}
        >
          <Input
            value={value}
            onChange={(event) => setValue(event.target.value)}
            maxLength={120}
            autoFocus
            aria-label="Session name"
            aria-invalid={Boolean(error)}
            aria-describedby={error ? "rename-session-error" : undefined}
          />
          {error ? (
            <p id="rename-session-error" role="alert" className="mt-2 text-caption-11 text-status-danger">
              {error}
            </p>
          ) : null}
          <DialogFooter className="mt-4">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!value.trim()}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
