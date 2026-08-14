import { useState } from "react";
import { NoteEditor } from "./NoteEditor";
import { NotesDashboard } from "./NotesDashboard";

export function NotesView() {
  const [openId, setOpenId] = useState<string | null>(null);
  const [refreshToken, setRefreshToken] = useState(0);

  if (openId) {
    return (
      <NoteEditor
        key={openId}
        noteId={openId}
        onBack={() => setOpenId(null)}
        onDeleted={() => {
          setOpenId(null);
          setRefreshToken((t) => t + 1);
        }}
        onSaved={() => setRefreshToken((t) => t + 1)}
      />
    );
  }

  return (
    <NotesDashboard
      refreshToken={refreshToken}
      onOpenNote={setOpenId}
      onChanged={() => setRefreshToken((t) => t + 1)}
    />
  );
}
