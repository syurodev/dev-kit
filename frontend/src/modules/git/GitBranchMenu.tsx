import { gitUI, useGitBusy, useGitOperation } from "./gitUI";
import { useModuleFolder } from "@/stores/workFolder";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { Field, FieldLabel } from "@/components/ui/field";
import { InlineFeedback, PendingStatus } from "@/components/ui/feedback";
import { useState, useId, useEffect } from "react";
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
  disabled = false,
  dirty,
  onChanged,
  onError,
}: {
  /** "" for a detached HEAD. */
  branch: string;
  disabled?: boolean;
  /** Staged, unstaged or conflicted changes are in the way of a switch. */
  dirty: boolean;
  onChanged: () => void;
  onError: (message: string) => void;
}) {
  // null until the menu has been opened once: branches are read when someone
  // is about to choose one, not on every render of the bar.
  const { folder } = useModuleFolder("git");
  const busy = useGitBusy();
  const visible = useModuleVisibility();
  const [branches, setBranches] = useState<Branch[] | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => {
    if (!visible) setMenuOpen(false);
  }, [visible]);
  const [creating, setCreating] = useState(false);

  function read(open: boolean) {
    setMenuOpen(open);
    if (!open) return;
    gitBranches()
      .then(setBranches)
      .catch((e: Error) => onError(e.message));
  }

  function switchTo(name: string) {
    void gitUI
      .run(folder, "Switching branch…", () => gitCheckout(name), `Switched to ${name}`)
      .then((ok) => {
        if (ok) onChanged();
        else onError(gitUI.useStore.getState().operations[folder]?.error ?? "");
      });
  }

  return (
    <>
      <DropdownMenu open={menuOpen && visible} onOpenChange={read}>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            type="button"
            disabled={disabled || busy}
            aria-label={`Branch: ${branch || "detached HEAD"}. Switch branch`}
            className="flex min-w-0 items-center gap-2.5 rounded-md text-text-primary hover:text-text-secondary"
          >
            <GitBranch className="size-4 shrink-0 text-text-secondary" aria-hidden />
            <span className="truncate text-title-14">{branch || "(detached)"}</span>
            <ChevronDown className="size-(--icon-14) shrink-0 text-text-tertiary" aria-hidden />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="max-h-80 w-96 max-w-screen-90">
          <DropdownMenuLabel>{dirty ? "Commit or stash to switch" : "Switch branch"}</DropdownMenuLabel>
          {branches === null ? (
            <DropdownMenuItem disabled>Reading branches…</DropdownMenuItem>
          ) : (
            branches.map((candidate) => (
              <DropdownMenuItem
                key={candidate.name}
                icon={candidate.current ? <Check /> : undefined}
                // The branch you are on is not dimmed: it is the one thing in
                // the list that is true. Only the others wait on a clean tree.
                disabled={busy || (dirty && !candidate.current)}
                title={candidate.upstream ? `${candidate.name} → ${candidate.upstream}` : candidate.name}
                onSelect={() => {
                  if (!candidate.current) switchTo(candidate.name);
                }}
              >
                <span className="font-mono text-code-12">{candidate.name}</span>
              </DropdownMenuItem>
            ))
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={busy} icon={<Plus />} onSelect={() => setCreating(true)}>
            New branch…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <NewBranchDialog
        open={creating && visible}
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
  const nameId = useId();
  const [name, setName] = useState("");
  const { folder } = useModuleFolder("git");
  const busy = useGitBusy();
  const operation = useGitOperation(folder);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) setError("");
  }, [open]);

  function create() {
    const trimmed = name.trim();
    if (!trimmed) return;
    if (busy) return;
    setError("");
    void gitUI
      .run(folder, "Creating branch…", () => gitCreateBranch(trimmed), `Created ${trimmed}`)
      .then((ok) => {
        if (ok) {
          setName("");
          onCreated();
        } else setError(gitUI.useStore.getState().operations[folder]?.error ?? "");
      });
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next || busy) return;
        setError("");
        onClose();
      }}
    >
      <DialogContent showCloseButton={!busy} className="gap-4 p-5 sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="text-title-14 text-text-primary">New branch</DialogTitle>
          <DialogDescription className="text-body-12 text-text-secondary">
            Starts from {from || "the current commit"} and switches to it. Your changes come with you.
          </DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor={nameId}>Branch name</FieldLabel>
          <Input
            id={nameId}
            disabled={busy}
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
        </Field>
        {busy && <PendingStatus>{operation.label}</PendingStatus>}
        {error ? <InlineFeedback>{error}</InlineFeedback> : null}
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
