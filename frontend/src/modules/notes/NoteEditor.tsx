import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowLeft, Bold, Code, Italic, Strikethrough, Trash2, Underline, X } from "lucide-react";
import { KEYS } from "platejs";
import { Plate, usePlateEditor } from "platejs/react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Editor, EditorContainer } from "@/components/ui/editor";
import { FloatingToolbar } from "@/components/ui/floating-toolbar";
import { Input } from "@/components/ui/input";
import { LinkToolbarButton } from "@/components/ui/link-toolbar-button";
import { MarkToolbarButton } from "@/components/ui/mark-toolbar-button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { errorMessage } from "../../bridge/errors";
import {
  deleteNote,
  getNote,
  listFolders,
  setNoteOrganization,
  updateNote,
  type Note,
  type NoteFolder,
} from "../../bridge/notes";
import { folderDotVar } from "./folderColors";
import { tagChipVar } from "./tagColor";
import { NotesEditorKit } from "./plate/plugins";

type SaveStatus = "idle" | "saving" | "saved" | "error";

type NoteEditorProps = {
  noteId: string;
  onBack?: () => void;
  onDeleted: () => void;
  onSaved: () => void;
};

const AUTOSAVE_DELAY_MS = 1500;
const MAX_TAGS = 32;
const MAX_TAG_LEN = 40;

function inheritedTags(note: Note): string[] {
  const own = new Set(note.tags.map((t) => t.toLowerCase()));
  return note.effectiveTags.filter((t) => !own.has(t.toLowerCase()));
}

function TagChip({
  tag,
  onRemove,
  readOnly,
}: {
  tag: string;
  onRemove?: () => void;
  readOnly?: boolean;
}) {
  return (
    <Badge variant="secondary" className="gap-1.5 bg-muted text-foreground">
      <span
        className="size-1.5 shrink-0 rounded-full"
        style={{ background: tagChipVar(tag) }}
        aria-hidden
      />
      {tag}
      {onRemove && !readOnly ? (
        <button
          type="button"
          aria-label={`Remove ${tag}`}
          className="inline-flex"
          onClick={onRemove}
        >
          <X className="size-3" />
        </button>
      ) : null}
    </Badge>
  );
}

export function NoteEditor({ noteId, onBack, onDeleted, onSaved }: NoteEditorProps) {
  const [note, setNote] = useState<Note | null>(null);
  const [folders, setFolders] = useState<NoteFolder[]>([]);
  const [title, setTitle] = useState("");
  const [folderId, setFolderId] = useState("");
  const [ownTags, setOwnTags] = useState<string[]>([]);
  const [tagDraft, setTagDraft] = useState("");
  const [status, setStatus] = useState<SaveStatus>("idle");
  const [loadError, setLoadError] = useState("");
  const [saveError, setSaveError] = useState("");
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const saveInFlight = useRef(false);
  const pendingSave = useRef(false);
  const pendingTitle = useRef("");
  const titleRef = useRef("");
  const noteRef = useRef<Note | null>(null);
  const orgInFlight = useRef(false);

  const editor = usePlateEditor({
    plugins: NotesEditorKit,
    value: [],
  });

  const folderName = useMemo(() => {
    if (!folderId) {
      return "Ungrouped";
    }
    return folders.find((f) => f.id === folderId)?.name ?? "Ungrouped";
  }, [folderId, folders]);

  const inherited = useMemo(
    () => (note ? inheritedTags({ ...note, tags: ownTags }) : []),
    [note, ownTags],
  );

  useEffect(() => {
    let mounted = true;
    setLoadError("");
    setStatus("idle");
    Promise.all([getNote(noteId), listFolders()])
      .then(([n, folderList]) => {
        if (!mounted) {
          return;
        }
        noteRef.current = n;
        setNote(n);
        setFolders(folderList);
        titleRef.current = n.title;
        setTitle(n.title);
        setFolderId(n.folderId);
        setOwnTags(n.tags);
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
      const loadedNote = noteRef.current;
      if (loadedNote) {
        void updateNote(
          loadedNote.id,
          titleRef.current,
          editor.api.markdown.serialize(),
        );
      }
    };
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
      noteRef.current = { ...updated, tags: ownTags, folderId };
      setNote(noteRef.current);
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

  async function persistOrganization(nextFolderId: string, nextTags: string[]) {
    if (!note || orgInFlight.current) {
      return;
    }
    orgInFlight.current = true;
    try {
      const summary = await setNoteOrganization(note.id, nextFolderId, nextTags);
      const refreshed = {
        ...noteRef.current!,
        folderId: summary.folderId,
        tags: summary.tags,
        effectiveTags: summary.effectiveTags,
      };
      noteRef.current = refreshed;
      setNote(refreshed);
      setFolderId(summary.folderId);
      setOwnTags(summary.tags);
      onSaved();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      orgInFlight.current = false;
    }
  }

  function tryAddTag(raw: string) {
    const tag = raw.trim();
    if (!tag) {
      return;
    }
    if (tag.length > MAX_TAG_LEN) {
      toast.error("Tag must be at most 40 characters.");
      return;
    }
    const key = tag.toLowerCase();
    if (ownTags.some((t) => t.toLowerCase() === key)) {
      setTagDraft("");
      return;
    }
    if (ownTags.length >= MAX_TAGS) {
      toast.error("A note can have at most 32 tags.");
      return;
    }
    const next = [...ownTags, tag];
    setOwnTags(next);
    setTagDraft("");
    void persistOrganization(folderId, next);
  }

  function removeOwnTag(tag: string) {
    const next = ownTags.filter((t) => t !== tag);
    setOwnTags(next);
    void persistOrganization(folderId, next);
  }

  function onFolderChange(value: string) {
    const nextFolderId = value === "ungrouped" ? "" : value;
    setFolderId(nextFolderId);
    void persistOrganization(nextFolderId, ownTags);
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
        {onBack ? (
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            aria-label="Back to notes"
            onClick={onBack}
          >
            <ArrowLeft className="size-4" />
          </Button>
        ) : null}
        <nav aria-label="Note breadcrumb" className="flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
          <button type="button" className="shrink-0 hover:text-foreground" onClick={onBack}>
            Notes
          </button>
          <span aria-hidden>/</span>
          <span className="truncate">{folderName}</span>
          <span aria-hidden>/</span>
          <span className="truncate text-foreground">{title || "Untitled"}</span>
        </nav>
        <span className="ml-auto shrink-0 text-xs text-muted-foreground">
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
      <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-2">
        <Input
          aria-label="note-title"
          value={title}
          onChange={(e) => {
            titleRef.current = e.target.value;
            setTitle(e.target.value);
            scheduleSave(e.target.value);
          }}
          className="h-8 min-w-[12rem] flex-1 border-none px-0 text-base font-semibold shadow-none focus-visible:ring-0"
        />
        <Select value={folderId || "ungrouped"} onValueChange={onFolderChange}>
          <SelectTrigger size="sm" className="w-[10rem]" aria-label="note-folder">
            <SelectValue placeholder="Folder" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="ungrouped">Ungrouped</SelectItem>
            {folders.map((folder) => (
              <SelectItem key={folder.id} value={folder.id}>
                <span className="flex items-center gap-2">
                  <span
                    className="size-2 shrink-0 rounded-full"
                    style={{ background: folderDotVar(folder.color) }}
                    aria-hidden
                  />
                  {folder.name}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          aria-label="Add tag"
          placeholder="Add tag…"
          value={tagDraft}
          onChange={(e) => setTagDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === ",") {
              e.preventDefault();
              tryAddTag(tagDraft);
            }
          }}
          onBlur={() => {
            if (tagDraft.trim()) {
              tryAddTag(tagDraft);
            }
          }}
          className="h-8 w-[10rem]"
        />
      </div>
      {(ownTags.length > 0 || inherited.length > 0) && (
        <div className="space-y-1 border-b border-border px-4 py-2">
          {ownTags.length > 0 ? (
            <div className="flex flex-wrap gap-1">
              {ownTags.map((tag) => (
                <TagChip key={tag} tag={tag} onRemove={() => removeOwnTag(tag)} />
              ))}
            </div>
          ) : null}
          {inherited.length > 0 ? (
            <div className="flex flex-wrap items-center gap-1">
              <span className="text-xs text-muted-foreground">From folder:</span>
              {inherited.map((tag) => (
                <TagChip key={tag} tag={tag} readOnly />
              ))}
            </div>
          ) : null}
        </div>
      )}
      {saveError ? (
        <p role="alert" className="border-b border-border px-4 py-1 text-xs text-destructive">
          {saveError}
        </p>
      ) : null}
      <Plate editor={editor} onChange={() => scheduleSave(title)}>
        <EditorContainer className="flex-1 overflow-auto">
          <Editor placeholder="Write something..." className="px-8 py-6" />
          <FloatingToolbar>
            <MarkToolbarButton nodeType={KEYS.bold} tooltip="Bold">
              <Bold />
            </MarkToolbarButton>
            <MarkToolbarButton nodeType={KEYS.italic} tooltip="Italic">
              <Italic />
            </MarkToolbarButton>
            <MarkToolbarButton nodeType={KEYS.underline} tooltip="Underline">
              <Underline />
            </MarkToolbarButton>
            <MarkToolbarButton nodeType={KEYS.strikethrough} tooltip="Strikethrough">
              <Strikethrough />
            </MarkToolbarButton>
            <MarkToolbarButton nodeType={KEYS.code} tooltip="Code">
              <Code />
            </MarkToolbarButton>
            <LinkToolbarButton />
          </FloatingToolbar>
        </EditorContainer>
      </Plate>
    </div>
  );
}
