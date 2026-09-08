import { useCallback, useEffect, useState } from "react";
import { RotateCcw, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
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
import { Badge } from "@/components/ui/badge";
import {
  emptyTrash,
  listTrashedNotes,
  purgeNote,
  restoreNote,
  type TrashedNote,
} from "../../bridge/notes";
import { errorMessage } from "../../bridge/errors";

/** Whole days until a trashed note is purged, floored — "purges in 0 days"
 *  would be wrong for something that still has hours left. */
export function daysUntilPurge(purgesAt: string, now: number = Date.now()): number {
  const remaining = new Date(purgesAt).getTime() - now;
  if (!Number.isFinite(remaining)) return 0;
  return Math.max(0, Math.floor(remaining / 86_400_000));
}

/** Figma `Notes / 02 Dashboard — Trash` and `Notes / Trash Content`. */
export function NotesTrash({ onChanged }: { onChanged: () => void }) {
  const [items, setItems] = useState<TrashedNote[]>([]);
  const [error, setError] = useState("");
  const [confirmEmpty, setConfirmEmpty] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setItems(await listTrashedNotes());
      setError("");
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function act(run: () => Promise<unknown>) {
    try {
      await run();
      await refresh();
      onChanged();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-2.5">
        <span className="text-body-12 text-text-tertiary">
          items in trash are deleted permanently after 30 days
        </span>
        <Button
          variant="outline"
          size="sm"
          disabled={items.length === 0}
          onClick={() => setConfirmEmpty(true)}
        >
          <Trash2 />
          Empty trash
        </Button>
      </div>

      {error && (
        <div className="border-b border-status-danger/30 bg-status-danger/10 px-4 py-2 text-body-12 text-status-danger">
          {error}
        </div>
      )}

      <div className="flex-1 overflow-auto p-4">
        {items.length === 0 ? (
          <p className="pt-10 text-center text-body-13 text-text-tertiary">Trash is empty.</p>
        ) : (
          <ul className="space-y-2">
            {items.map((note) => (
              <li
                key={note.id}
                className="flex items-start gap-3 rounded-lg border border-border bg-surface-raised p-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-body-13-medium text-text-primary">
                      {note.title || "Untitled"}
                    </span>
                    {note.effectiveTags.slice(0, 1).map((tag) => (
                      <Badge key={tag} variant="outline" className="text-label-12">
                        {tag}
                      </Badge>
                    ))}
                  </div>
                  <p className="mt-0.5 line-clamp-1 text-body-12 text-text-secondary">
                    {note.preview}
                  </p>
                  <p className="mt-1 text-caption-11 text-text-tertiary">
                    purges in {daysUntilPurge(note.purgesAt)} days
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  <Button size="sm" variant="outline" onClick={() => void act(() => restoreNote(note.id))}>
                    <RotateCcw />
                    Restore
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => void act(() => purgeNote(note.id))}
                  >
                    Delete forever
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      <AlertDialog open={confirmEmpty} onOpenChange={setConfirmEmpty}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Empty the trash?</AlertDialogTitle>
            <AlertDialogDescription>
              {items.length} note{items.length === 1 ? "" : "s"} will be deleted for good,
              along with their version history. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => void act(emptyTrash)}>
              Empty trash
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
