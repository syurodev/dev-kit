import { useState } from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Search, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
import { cn } from "@/lib/utils";
import {
  emptyTrash,
  listTrashedNotes,
  purgeNote,
  restoreNote,
} from "../../bridge/notes";
import { errorMessage } from "../../bridge/errors";
import { noteCardSurface } from "./noteColor";

/** Whole days until a trashed note is purged, floored — "purges in 0 days"
 *  would be wrong for something that still has hours left. */
export function daysUntilPurge(purgesAt: string, now: number = Date.now()): number {
  const remaining = new Date(purgesAt).getTime() - now;
  if (!Number.isFinite(remaining)) return 0;
  return Math.max(0, Math.floor(remaining / 86_400_000));
}

/**
 * Figma `Notes / Trash Content` (807:3654): a search field, the 30-day rule
 * in Code/11, and Empty trash as a danger-soft button, over the same card
 * grid as the dashboard — each card in its folder colour with "purges in N
 * days" (warning; danger once it is close) and Restore / Delete at 24px.
 */
export function NotesTrash({ onChanged }: { onChanged: () => void }) {
  const [trashListRef] = useAutoAnimate<HTMLUListElement>({ duration: 180, easing: "ease-out" });
  const queryClient = useQueryClient();
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  const [confirmEmpty, setConfirmEmpty] = useState(false);

  const { data: items = [] } = useQuery({
    queryKey: ["notes", "trash"],
    queryFn: () => listTrashedNotes(),
  });

  async function act(run: () => Promise<unknown>) {
    try {
      await run();
      await queryClient.invalidateQueries({ queryKey: ["notes"] });
      onChanged();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  const needle = query.trim().toLowerCase();
  const shown = needle
    ? items.filter(
        (n) => n.title.toLowerCase().includes(needle) || n.preview.toLowerCase().includes(needle),
      )
    : items;

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-3 px-6 pt-5 pb-7">
        <div className="relative w-90">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-3.5 -translate-y-1/2 text-text-tertiary" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search trash…"
            aria-label="Search trash"
            className="h-8 border-border-subtle bg-surface-well pl-8.5"
          />
        </div>
        <span className="text-code-11 text-text-tertiary">
          Items in trash are deleted permanently after 30 days.
        </span>
        <span className="flex-1" />
        <Button
          variant="outline"
          className="border-(--border-danger) bg-(--feedback-danger-soft) pr-4 pl-3.5 text-status-danger hover:bg-(--feedback-danger-soft) hover:text-status-danger"
          disabled={items.length === 0}
          onClick={() => setConfirmEmpty(true)}
        >
          <Trash2 />
          Empty trash
        </Button>
      </div>

      {error && (
        <div className="mx-6 mb-4 rounded-md border border-(--border-danger) bg-(--feedback-danger-soft) px-4 py-2 text-body-12 text-status-danger">
          {error}
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-auto">
        {shown.length === 0 ? (
          <p className="pt-10 text-center text-body-13 text-text-tertiary">
            {items.length === 0 ? "Trash is empty." : "Nothing in the trash matches."}
          </p>
        ) : (
          <ul ref={trashListRef} className="grid grid-cols-[repeat(auto-fill,minmax(348px,1fr))] gap-5 px-6 pb-6">
            {shown.map((note) => {
              const days = daysUntilPurge(note.purgesAt);
              return (
                <li
                  key={note.id}
                  className="flex h-37 flex-col gap-2.5 rounded-control border p-4"
                  style={noteCardSurface(note.effectiveColor)}
                >
                  <div className="flex items-center gap-2">
                    <span className="min-w-0 flex-1 truncate text-title-16 text-text-primary">
                      {note.title || "Untitled"}
                    </span>
                    <MoreHorizontal className="size-3.5 shrink-0 text-text-tertiary" />
                  </div>
                  <p className="min-h-0 flex-1 overflow-hidden text-body-13 text-text-secondary line-clamp-2">
                    {note.preview}
                  </p>
                  <div className="flex items-center gap-1.5">
                    <span
                      className={cn(
                        "text-code-11",
                        days <= 3 ? "text-status-danger" : "text-status-warning",
                      )}
                    >
                      purges in {days} day{days === 1 ? "" : "s"}
                    </span>
                    <span className="flex-1" />
                    <Button
                      size="xs"
                      variant="outline"
                      className="bg-surface-raised text-caption-11-medium"
                      onClick={() => void act(() => restoreNote(note.id))}
                    >
                      Restore
                    </Button>
                    <Button
                      size="xs"
                      variant="outline"
                      className="border-(--border-danger) bg-surface-raised text-caption-11-medium text-status-danger hover:text-status-danger"
                      onClick={() => void act(() => purgeNote(note.id))}
                    >
                      Delete
                    </Button>
                  </div>
                </li>
              );
            })}
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
