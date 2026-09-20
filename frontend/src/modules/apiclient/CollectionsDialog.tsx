import { useEffect, useState } from "react";
import { FolderPlus, Trash2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  deleteCollection,
  listCollections,
  saveCollection,
  type ApiCollection,
} from "@/bridge/apiclient";
import { folderPath, foldersByPath } from "./collections";

/**
 * The folders requests are filed in.
 *
 * Deleting one that still holds something is refused by the backend and the
 * message names what is inside — it is shown here as written rather than
 * reduced to "could not delete", because the next step is in the sentence.
 */
export function CollectionsDialog({
  open,
  onClose,
  onChanged,
}: {
  open: boolean;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [collections, setCollections] = useState<ApiCollection[] | null>(null);
  const [names, setNames] = useState<Record<string, string>>({});
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    setError("");
    listCollections()
      .then((next) => {
        setCollections(next);
        setNames(Object.fromEntries(next.map((folder) => [folder.id, folder.name])));
      })
      .catch((e: Error) => setError(e.message));
  }, [open]);

  function reload() {
    listCollections()
      .then((next) => {
        setCollections(next);
        setNames(Object.fromEntries(next.map((folder) => [folder.id, folder.name])));
        onChanged();
      })
      .catch((e: Error) => setError(e.message));
  }

  function run(work: Promise<unknown>) {
    setError("");
    work.then(reload).catch((e: Error) => setError(e.message));
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-[560px]" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>Folders</DialogTitle>
        </DialogHeader>

        {collections === null ? (
          <p className="text-body-13 text-text-secondary">Loading…</p>
        ) : collections.length === 0 ? (
          <p className="text-body-13 text-text-secondary">
            No folders yet. Requests without one sit at the top of the list.
          </p>
        ) : (
          <ul className="flex max-h-80 flex-col gap-2 overflow-auto">
            {foldersByPath(collections).map((folder) => (
              <li key={folder.id} className="flex items-center gap-2">
                <span
                  className="w-40 shrink-0 truncate text-code-11 text-text-tertiary"
                  title={folderPath(collections, folder.id)}
                >
                  {folderPath(collections, folder.parentId ?? "") || "—"}
                </span>
                <Input
                  value={names[folder.id] ?? ""}
                  onChange={(e) => setNames({ ...names, [folder.id]: e.target.value })}
                  onBlur={() => {
                    const name = (names[folder.id] ?? "").trim();
                    // An empty name is refused by the backend; putting the old
                    // one back is a better answer than an error about a field
                    // somebody cleared on their way to typing.
                    if (!name) {
                      setNames({ ...names, [folder.id]: folder.name });
                      return;
                    }
                    if (name !== folder.name) run(saveCollection({ ...folder, name }));
                  }}
                  aria-label={`Name of ${folder.name}`}
                />
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Delete ${folder.name}`}
                  onClick={() => run(deleteCollection(folder.id))}
                >
                  <Trash2 className="size-4" aria-hidden />
                </Button>
              </li>
            ))}
          </ul>
        )}

        {error ? (
          <p className="text-code-11 text-status-danger" role="alert">
            {error}
          </p>
        ) : null}

        <DialogFooter className="sm:justify-between">
          <Button
            variant="outline"
            size="sm"
            onClick={() => run(saveCollection({ name: "New folder" }))}
          >
            <FolderPlus className="size-4" aria-hidden />
            New folder
          </Button>
          <Button size="sm" onClick={onClose}>
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
