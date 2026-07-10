import { useEffect, useState } from "react";
import {
  createNote,
  deleteNote,
  getNote,
  listNotes,
  type Note,
  type NoteSummary,
} from "../../bridge/notes";

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function NotesView() {
  const [notes, setNotes] = useState<NoteSummary[]>([]);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [selected, setSelected] = useState<Note | null>(null);
  const [error, setError] = useState("");

  async function refresh() {
    try {
      setNotes(await listNotes());
    } catch (e) {
      setError(message(e));
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function onCreate() {
    setError("");
    try {
      await createNote(title, body);
      setTitle("");
      setBody("");
      await refresh();
    } catch (e) {
      setError(message(e));
    }
  }

  async function onOpen(id: string) {
    setError("");
    try {
      setSelected(await getNote(id));
    } catch (e) {
      setError(message(e));
    }
  }

  async function onDelete(id: string) {
    setError("");
    try {
      await deleteNote(id);
      if (selected?.id === id) {
        setSelected(null);
      }
      await refresh();
    } catch (e) {
      setError(message(e));
    }
  }

  return (
    <section className="notes-view">
      <h2>Notes</h2>
      <div className="notes-create">
        <input
          aria-label="note-title"
          placeholder="Title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
        <textarea
          aria-label="note-body"
          placeholder="Write something secret…"
          value={body}
          onChange={(e) => setBody(e.target.value)}
        />
        <button type="button" onClick={onCreate}>
          Add encrypted note
        </button>
      </div>

      {error ? <p role="alert">{error}</p> : null}

      <ul className="notes-list">
        {notes.map((note) => (
          <li key={note.id}>
            <button type="button" onClick={() => onOpen(note.id)}>
              {note.title}
            </button>
            <button
              type="button"
              aria-label={`delete-${note.title}`}
              onClick={() => onDelete(note.id)}
            >
              Delete
            </button>
          </li>
        ))}
      </ul>

      {selected ? (
        <article className="notes-detail">
          <h3>{selected.title}</h3>
          <pre aria-label="note-detail-body">{selected.body}</pre>
        </article>
      ) : null}
    </section>
  );
}
