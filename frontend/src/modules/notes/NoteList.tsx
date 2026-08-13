import { useEffect, useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { createNote, listNotes, searchNotes, type NoteSummary } from "../../bridge/notes";
import { errorMessage } from "../../bridge/errors";

type NoteListProps = {
  selectedId: string | null;
  onSelect: (id: string) => void;
  refreshToken: number;
};

export function NoteList({ selectedId, onSelect, refreshToken }: NoteListProps) {
  const [notes, setNotes] = useState<NoteSummary[]>([]);
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);

  async function refresh(q: string) {
    try {
      const list = q.trim() ? await searchNotes(q) : await listNotes();
      setNotes(list);
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  useEffect(() => {
    void refresh(query);
    // Intentionally omits `query` — this effect re-runs on mount and on
    // every refreshToken bump (create/delete/save elsewhere), using
    // whatever query is current at that moment; typing itself triggers
    // its own refresh via onChange below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshToken]);

  async function onCreate() {
    setError("");
    setCreating(true);
    try {
      const created = await createNote("Untitled", "");
      await refresh(query);
      onSelect(created.id);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setCreating(false);
    }
  }

  return (
    <div className="flex h-full w-64 shrink-0 flex-col border-r border-border">
      <div className="flex items-center gap-2 border-b border-border p-2">
        <Input
          aria-label="note-search"
          placeholder="Search notes..."
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            void refresh(e.target.value);
          }}
          className="h-8"
        />
        <Button
          type="button"
          size="icon-sm"
          variant="outline"
          aria-label="New note"
          disabled={creating}
          onClick={() => void onCreate()}
        >
          <Plus className="size-4" />
        </Button>
      </div>
      {error ? (
        <p role="alert" className="px-3 py-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
      <ScrollArea className="flex-1">
        <div className="flex flex-col gap-0.5 p-2">
          {notes.map((note) => (
            <button
              key={note.id}
              type="button"
              onClick={() => onSelect(note.id)}
              className={cn(
                "flex flex-col items-start gap-0.5 rounded-lg px-3 py-2 text-left",
                note.id === selectedId ? "bg-accent" : "hover:bg-accent/60",
              )}
            >
              <span className="w-full truncate text-sm font-medium text-foreground">
                {note.title || "Untitled"}
              </span>
              <span className="text-xs text-muted-foreground">{note.updatedAt}</span>
            </button>
          ))}
          {notes.length === 0 ? (
            <p className="px-3 py-2 text-xs text-muted-foreground">No notes yet.</p>
          ) : null}
        </div>
      </ScrollArea>
    </div>
  );
}
