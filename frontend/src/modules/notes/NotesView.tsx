import { useEffect, useState } from "react";
import {
  createNote,
  deleteNote,
  getNote,
  listNotes,
  searchNotes,
  type Note,
  type NoteSummary,
} from "../../bridge/notes";
import { errorMessage } from "../../bridge/errors";

export function NotesView() {
  const [notes, setNotes] = useState<NoteSummary[]>([]);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [selected, setSelected] = useState<Note | null>(null);
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");

  async function refresh(q = query) {
    try {
      setNotes(q.trim() ? await searchNotes(q) : await listNotes());
    } catch (e) {
      setError(errorMessage(e));
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
      setError(errorMessage(e));
    }
  }

  async function onOpen(id: string) {
    setError("");
    try {
      setSelected(await getNote(id));
    } catch (e) {
      setError(errorMessage(e));
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
      setError(errorMessage(e));
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

      <input
        aria-label="note-search"
        className="notes-search"
        placeholder="Search notes (title always, body when unlocked)…"
        value={query}
        onChange={(e) => {
          setQuery(e.target.value);
          void refresh(e.target.value);
        }}
      />

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
