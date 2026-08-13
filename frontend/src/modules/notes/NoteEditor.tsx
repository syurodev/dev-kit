import { useEffect, useRef, useState } from "react";
import { Trash2 } from "lucide-react";
import { Plate, usePlateEditor } from "platejs/react";
import { Button } from "@/components/ui/button";
import { Editor, EditorContainer } from "@/components/ui/editor";
import { Input } from "@/components/ui/input";
import { deleteNote, getNote, updateNote, type Note } from "../../bridge/notes";
import { errorMessage } from "../../bridge/errors";
import { NotesEditorKit } from "./plate/plugins";

type SaveStatus = "idle" | "saving" | "saved" | "error";

type NoteEditorProps = {
  noteId: string;
  onDeleted: () => void;
  onSaved: () => void;
};

const AUTOSAVE_DELAY_MS = 1500;

export function NoteEditor({ noteId, onDeleted, onSaved }: NoteEditorProps) {
  const [note, setNote] = useState<Note | null>(null);
  const [title, setTitle] = useState("");
  const [status, setStatus] = useState<SaveStatus>("idle");
  const [loadError, setLoadError] = useState("");
  const [saveError, setSaveError] = useState("");
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const saveInFlight = useRef(false);
  const pendingSave = useRef(false);
  const pendingTitle = useRef("");

  const editor = usePlateEditor({
    plugins: NotesEditorKit,
    value: [],
  });

  useEffect(() => {
    let mounted = true;
    setLoadError("");
    setStatus("idle");
    getNote(noteId)
      .then((n) => {
        if (!mounted) {
          return;
        }
        setNote(n);
        setTitle(n.title);
        const value = editor.api.markdown.deserialize(n.body);
        editor.tf.setValue(value);
      })
      .catch((e) => {
        if (mounted) {
          setLoadError(errorMessage(e));
        }
      });
    return () => {
      mounted = false;
      if (saveTimer.current) {
        clearTimeout(saveTimer.current);
      }
    };
    // Runs once per mount — NotesView remounts this component (via `key`)
    // whenever the selected note changes, so `noteId` is stable for the
    // lifetime of any given instance.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [noteId]);

  function scheduleSave(nextTitle: string) {
    if (saveTimer.current) {
      clearTimeout(saveTimer.current);
    }
    saveTimer.current = setTimeout(() => void save(nextTitle), AUTOSAVE_DELAY_MS);
  }

  async function save(nextTitle: string) {
    if (!note) {
      return;
    }
    if (saveInFlight.current) {
      pendingSave.current = true;
      pendingTitle.current = nextTitle;
      return;
    }
    saveInFlight.current = true;
    setStatus("saving");
    setSaveError("");
    try {
      const body = editor.api.markdown.serialize();
      const updated = await updateNote(note.id, nextTitle, body);
      setNote(updated);
      setStatus("saved");
      onSaved();
    } catch (e) {
      setStatus("error");
      setSaveError(errorMessage(e));
    } finally {
      saveInFlight.current = false;
      if (pendingSave.current) {
        pendingSave.current = false;
        void save(pendingTitle.current);
      }
    }
  }

  async function onDelete() {
    if (!note) {
      return;
    }
    try {
      await deleteNote(note.id);
      onDeleted();
    } catch (e) {
      setSaveError(errorMessage(e));
    }
  }

  if (loadError) {
    return (
      <div className="flex flex-1 items-center justify-center p-4">
        <p role="alert" className="text-sm text-destructive">
          {loadError}
        </p>
      </div>
    );
  }

  if (!note) {
    return (
      <div className="flex flex-1 items-center justify-center p-4">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-1 flex-col">
      <div className="flex items-center gap-2 border-b border-border px-4 py-2">
        <Input
          aria-label="note-title"
          value={title}
          onChange={(e) => {
            setTitle(e.target.value);
            scheduleSave(e.target.value);
          }}
          className="h-8 flex-1 border-none px-0 text-base font-semibold shadow-none focus-visible:ring-0"
        />
        <span className="text-xs text-muted-foreground">
          {status === "saving" ? "Saving…" : status === "saved" ? "Saved" : null}
        </span>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label="Delete note"
          onClick={() => void onDelete()}
        >
          <Trash2 className="size-4" />
        </Button>
      </div>
      {saveError ? (
        <p role="alert" className="border-b border-border px-4 py-1 text-xs text-destructive">
          {saveError}
        </p>
      ) : null}
      <Plate editor={editor} onChange={() => scheduleSave(title)}>
        <EditorContainer className="flex-1 overflow-auto">
          <Editor placeholder="Write something..." className="px-8 py-6" />
        </EditorContainer>
      </Plate>
    </div>
  );
}
