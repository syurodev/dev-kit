import { useEffect, useState } from "react";
import { Folder, FolderPlus, X } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import {
  Item,
  ItemActions,
  ItemContent,
  ItemGroup,
  ItemMedia,
  ItemSeparator,
  ItemTitle,
} from "@/components/ui/item";
import { errorMessage } from "@/bridge/errors";
import {
  chooseWorkspaceFolder,
  createWorkspace,
  deleteWorkspace,
  listWorkspaceServices,
  updateWorkspace,
  type Workspace,
} from "@/bridge/workspace";
import {
  addFolder,
  makePrimary,
  removeFolder,
  rowsFromServices,
  type FolderRow,
} from "./workspaceDialogModel";

/**
 * Create or edit a workspace: its name, then the folders it is made of.
 *
 * Named first on purpose. Picking a folder straight from "Add workspace" made
 * the folder the workspace — its name, its one root — when a workspace is a
 * set of services that may live anywhere on the disk. Here the name is typed,
 * folders are added one at a time, and the first is the primary (its root).
 * Save sends the whole list; Go discovers what is new, keeps what is already
 * registered, and queues new services for their index.
 */
export function WorkspaceDialog({
  open,
  onOpenChange,
  workspace,
  onSaved,
  onRemoved,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The workspace to edit; null creates one. */
  workspace: Workspace | null;
  onSaved: (workspace: Workspace) => void;
  onRemoved?: (workspace: Workspace) => void;
}) {
  const editing = workspace !== null;
  const [name, setName] = useState("");
  const [instructions, setInstructions] = useState("");
  const [rows, setRows] = useState<FolderRow[]>([]);
  const [busy, setBusy] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);

  // Each opening starts from what is saved, not from the last edit left
  // behind by a Cancel.
  useEffect(() => {
    if (!open) return;
    setName(workspace?.name ?? "");
    setInstructions(workspace?.instructions ?? "");
    setRows([]);
    if (!workspace) return;
    let cancelled = false;
    listWorkspaceServices(workspace.id)
      .then((services) => {
        if (!cancelled) setRows(rowsFromServices(services, workspace.root));
      })
      .catch((e) => toast.error(errorMessage(e)));
    return () => {
      cancelled = true;
    };
  }, [open, workspace]);

  async function pickFolder() {
    try {
      const path = await chooseWorkspaceFolder(
        rows[0]?.path ?? workspace?.root ?? "",
      );
      if (path) setRows((prev) => addFolder(prev, path));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (!name.trim() || busy) return;
    setBusy(true);
    try {
      const paths = rows.map((row) => row.path);
      const result = workspace
        ? await updateWorkspace(workspace.id, name, paths, instructions)
        : await createWorkspace(name, paths, instructions);
      onSaved(result.workspace);
      onOpenChange(false);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!workspace) return;
    setBusy(true);
    try {
      await deleteWorkspace(workspace.id);
      onRemoved?.(workspace);
      setConfirmRemove(false);
      onOpenChange(false);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-h-[85vh] max-w-140 overflow-y-auto px-6 pt-5.5 pb-5 sm:max-w-140">
          <form onSubmit={save} className="flex flex-col">
            <DialogHeader>
              <DialogTitle className="text-title-16">
                {editing ? "Edit workspace" : "Create workspace"}
              </DialogTitle>
            </DialogHeader>

            <InputGroup className="mt-5 h-10">
              <InputGroupAddon className="self-stretch border-r border-border pr-2.5">
                <Folder />
              </InputGroupAddon>
              <InputGroupInput
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Workspace name"
                aria-label="Workspace name"
                autoFocus
              />
            </InputGroup>

            <p className="mt-4 mb-2 text-label-12 text-text-secondary">
              Source folders
            </p>
            {rows.length === 0 ? (
              <div className="flex flex-col items-center gap-2 rounded-lg border border-border px-4 py-6">
                <p className="text-body-13 text-text-tertiary">
                  Add a folder on{" "}
                  <span className="text-text-primary">this computer</span>
                </p>
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={() => void pickFolder()}
                >
                  <FolderPlus />
                  Add
                </Button>
              </div>
            ) : (
              <ItemGroup className="gap-0 rounded-lg border border-border has-data-[size=sm]:gap-0 has-data-[size=xs]:gap-0">
                {rows.map((row, index) => (
                  <FolderItem
                    key={row.path}
                    row={row}
                    primary={index === 0}
                    onMakePrimary={() =>
                      setRows((prev) => makePrimary(prev, row.path))
                    }
                    onRemove={() =>
                      setRows((prev) => removeFolder(prev, row.path))
                    }
                  />
                ))}
                <Item
                  asChild
                  size="sm"
                  className="h-12 rounded-none rounded-b-lg hover:bg-state-soft"
                >
                  <button type="button" onClick={() => void pickFolder()}>
                    <ItemMedia variant="icon" className="text-text-tertiary">
                      <FolderPlus />
                    </ItemMedia>
                    <ItemContent>
                      <ItemTitle className="font-normal">Add folder</ItemTitle>
                    </ItemContent>
                  </button>
                </Item>
              </ItemGroup>
            )}

            <label htmlFor="workspace-instructions" className="mt-5 mb-2 text-label-12 text-text-secondary">
              Instructions
            </label>
            <Textarea
              id="workspace-instructions"
              value={instructions}
              onChange={(e) => setInstructions(e.target.value)}
              disabled={busy}
              rows={5}
              className="max-h-60 min-h-28 resize-y field-sizing-fixed text-body-13"
              placeholder="Conventions, architecture decisions, and how agents should work in this workspace…"
              aria-describedby="workspace-instructions-help"
            />
            <p id="workspace-instructions-help" className="mt-2 text-label-12 text-text-tertiary">
              Shared by all agents in this workspace. Saved changes apply on their next turn.
            </p>

            <DialogFooter className="mx-0 mt-6 mb-0 items-center rounded-none border-t-0 bg-transparent p-0 sm:justify-between">
              {editing ? (
                <Button
                  type="button"
                  variant="destructive"
                  disabled={busy}
                  onClick={() => setConfirmRemove(true)}
                >
                  Remove workspace
                </Button>
              ) : (
                <span />
              )}
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => onOpenChange(false)}
                >
                  Cancel
                </Button>
                <Button type="submit" disabled={busy || !name.trim()}>
                  {editing
                    ? busy
                      ? "Saving…"
                      : "Save"
                    : busy
                      ? "Creating…"
                      : "Create workspace"}
                </Button>
              </div>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirmRemove} onOpenChange={setConfirmRemove}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove “{workspace?.name}”?</AlertDialogTitle>
            <AlertDialogDescription>
              DevKit forgets this workspace and its folder list on this
              computer. The folders and everything in them stay where they are.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={busy}
              onClick={() => void remove()}
            >
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/** One source folder: its name (full path on hover), Primary or a way to make it so, and remove. */
function FolderItem({
  row,
  primary,
  onMakePrimary,
  onRemove,
}: {
  row: FolderRow;
  primary: boolean;
  onMakePrimary: () => void;
  onRemove: () => void;
}) {
  return (
    <>
      <Item size="sm" className="group/folder h-12 rounded-none" title={row.path}>
        <ItemMedia variant="icon" className="text-text-tertiary">
          <Folder />
        </ItemMedia>
        <ItemContent className="min-w-0">
          <ItemTitle className="w-full truncate font-normal">
            {row.name}
          </ItemTitle>
        </ItemContent>
        <ItemActions>
          {primary ? (
            <Badge variant="outline">Primary</Badge>
          ) : (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              className="h-5 px-2 opacity-0 group-hover/folder:opacity-100 focus-visible:opacity-100"
              onClick={onMakePrimary}
            >
              Make primary
            </Button>
          )}
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={`Remove ${row.name}`}
            onClick={onRemove}
          >
            <X />
          </Button>
        </ItemActions>
      </Item>
      <ItemSeparator className="my-0" />
    </>
  );
}
