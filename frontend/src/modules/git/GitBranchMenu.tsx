import { useState } from "react";
import { Check, ChevronDown, GitBranch, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { gitBranches, gitCheckout, gitCreateBranch, type Branch } from "@/bridge/git";

/**
 * The branch Git is on, and the way to another: the name in the branch bar is
 * a menu of the repository's local branches, with a new one at the bottom.
 *
 * Switching is refused while there are staged, unstaged or conflicted changes —
 * the backend refuses, because a caller that is not looking would carry the
 * person's work onto another branch. The menu says so and disables the
 * branches rather than letting a click fail. A new branch is not gated: it
 * starts from where the work is, and carries it, which is the point of it.
 *
 * `onChanged` is called after a switch or a create, so everything that shows
 * the repository reads it again.
 */
export function GitBranchMenu({
  branch,
  dirty,
  onChanged,
  onError,
}: {
  /** "" for a detached HEAD. */
  branch: string;
  /** Staged, unstaged or conflicted changes are in the way of a switch. */
  dirty: boolean;
  onChanged: () => void;
  onError: (message: string) => void;
}) {
  // null until the menu has been opened once: branches are read when someone
  // is about to choose one, not on every render of the bar.
  const [branches, setBranches] = useState<Branch[] | null>(null);
  const [creating, setCreating] = useState(false);

  function read(open: boolean) {
    if (!open) return;
    gitBranches()
      .then(setBranches)
      .catch((e: Error) => onError(e.message));
  }

  function switchTo(name: string) {
    gitCheckout(name)
      .then(onChanged)
      .catch((e: Error) => onError(e.message));
  }

  return (
    <>
      <DropdownMenu onOpenChange={read}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            aria-label={`Branch: ${branch || "detached HEAD"}. Switch branch`}
            className="flex min-w-0 items-center gap-2.5 rounded-md text-text-primary hover:text-text-secondary"
          >
            <GitBranch className="size-4 shrink-0 text-text-secondary" aria-hidden />
            <span className="truncate text-title-14">{branch || "(detached)"}</span>
            <ChevronDown className="size-(--icon-14) shrink-0 text-text-tertiary" aria-hidden />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="max-h-80 w-72">
          <DropdownMenuLabel>{dirty ? "Commit or stash to switch" : "Switch branch"}</DropdownMenuLabel>
          {branches === null ? (
            <DropdownMenuItem disabled>Reading branches…</DropdownMenuItem>
          ) : (
            branches.map((candidate) => (
              <DropdownMenuItem
                key={candidate.name}
                icon={candidate.current ? <Check /> : undefined}
                detail={candidate.upstream || undefined}
                disabled={dirty || candidate.current}
                onSelect={() => switchTo(candidate.name)}
              >
                <span className="truncate font-mono text-code-12">{candidate.name}</span>
              </DropdownMenuItem>
            ))
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem icon={<Plus />} onSelect={() => setCreating(true)}>
            New branch…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <NewBranchDialog
        open={creating}
        from={branch}
        onClose={() => setCreating(false)}
        onCreated={() => {
          setCreating(false);
          onChanged();
        }}
      />
    </>
  );
}

function NewBranchDialog({
  open,
  from,
  onClose,
  onCreated,
}: {
  open: boolean;
  from: string;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function create() {
    const trimmed = name.trim();
    if (!trimmed) return;
    setBusy(true);
    setError("");
    gitCreateBranch(trimmed)
      .then(() => {
        setName("");
        onCreated();
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false));
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) return;
        setError("");
        onClose();
      }}
    >
      <DialogContent className="gap-4 p-5 sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="text-title-14 text-text-primary">New branch</DialogTitle>
          <DialogDescription className="text-body-12 text-text-secondary">
            Starts from {from || "the current commit"} and switches to it. Your changes come with you.
          </DialogDescription>
        </DialogHeader>
        <Input
          value={name}
          onChange={(event) => setName(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") create();
          }}
          placeholder="feat/my-change"
          aria-label="Branch name"
          className="h-8 font-mono text-code-12"
          autoFocus
        />
        {error ? (
          <p className="text-caption-11 text-status-danger" role="alert">
            {error}
          </p>
        ) : null}
        <DialogFooter className="-mx-5 -mb-5 px-5">
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={create} disabled={busy || name.trim() === ""}>
            Create branch
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
