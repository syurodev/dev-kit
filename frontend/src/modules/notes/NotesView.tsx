import { useState } from "react";
import { NoteEditor } from "./NoteEditor";
import { NoteList } from "./NoteList";

export function NotesView() {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [refreshToken, setRefreshToken] = useState(0);

  return (
    <div className="flex h-full">
      <NoteList selectedId={selectedId} onSelect={setSelectedId} refreshToken={refreshToken} />
      {selectedId ? (
        <NoteEditor
          key={selectedId}
          noteId={selectedId}
          onDeleted={() => {
            setSelectedId(null);
            setRefreshToken((t) => t + 1);
          }}
          onSaved={() => setRefreshToken((t) => t + 1)}
        />
      ) : (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          Select a note or create a new one.
        </div>
      )}
    </div>
  );
}
