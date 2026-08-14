import { useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  Bold,
  Code,
  Italic,
  PanelRight,
  Strikethrough,
  Trash2,
  Underline,
  X,
} from "lucide-react";
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
import { useSplitPanel } from "@/components/ui/split-panel";
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
import { useModuleAction } from "../../stores/moduleActions";
import { ColorPicker } from "./ColorPicker";
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

function NoteMetaPanel({
  folders,
  folderId,
  ownTags,
  color,
  tagDraft,
  inherited,
  onTagDraftChange,
  onFolderChange,
  onColorChange,
  onAddTag,
  onRemoveTag,
}: {
  folders: NoteFolder[];
  folderId: string;
  ownTags: string[];
  color: string;
  tagDraft: string;
  inherited: string[];
  onTagDraftChange: (value: string) => void;
  onFolderChange: (value: string) => void;
  onColorChange: (color: string) => void;
  onAddTag: (raw: string) => void;
  onRemoveTag: (tag: string) => void;
}) {
  return (
    <div className="flex flex-col gap-5 p-4">
      <div>
        <p className="mb-2 text-xs font-medium tracking-[0.4px] text-muted-foreground uppercase">
          Folder
        </p>
        <Select value={folderId || "ungrouped"} onValueChange={onFolderChange}>
          <SelectTrigger size="sm" className="w-full" aria-label="note-folder">
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
      </div>

      <div>
        <p className="mb-2 text-xs font-medium tracking-[0.4px] text-muted-foreground uppercase">
          Color
        </p>
        <ColorPicker value={color} onChange={onColorChange} allowInherit />
      </div>

      <div>
        <p className="mb-2 text-xs font-medium tracking-[0.4px] text-muted-foreground uppercase">
          Tags
        </p>
        <Input
          aria-label="Add tag"
          placeholder="Add tag…"
          value={tagDraft}
          onChange={(e) => onTagDraftChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === ",") {
              e.preventDefault();
              onAddTag(tagDraft);
            }
          }}
          onBlur={() => {
            if (tagDraft.trim()) {
              onAddTag(tagDraft);
            }
          }}
          className="h-8"
        />
        {ownTags.length > 0 || inherited.length > 0 ? (
          <div className="mt-2 space-y-1.5">
            {ownTags.length > 0 ? (
              <div className="flex flex-wrap gap-1">
                {ownTags.map((tag) => (
                  <TagChip key={tag} tag={tag} onRemove={() => onRemoveTag(tag)} />
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
        ) : null}
      </div>
    </div>
  );
}

export function NoteEditor({ noteId, onBack, onDeleted, onSaved }: NoteEditorProps) {
  const [note, setNote] = useState<Note | null>(null);
  const [folders, setFolders] = useState<NoteFolder[]>([]);
  const [title, setTitle] = useState("");
  const [folderId, setFolderId] = useState("");
  const [ownTags, setOwnTags] = useState<string[]>([]);
  const [color, setColor] = useState("");
  const [tagDraft, setTagDraft] = useState("");
  const [metaOpen, setMetaOpen] = useState(false);
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

  const { openPanel, closePanel } = useSplitPanel();

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
        setColor(n.color);
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
      const current = noteRef.current;
      noteRef.current = {
        ...updated,
        folderId: current?.folderId ?? folderId,
        tags: current?.tags ?? ownTags,
        effectiveTags: current?.effectiveTags ?? updated.effectiveTags,
        color: current?.color ?? color,
        effectiveColor: current?.effectiveColor ?? "",
      };
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

  async function persistOrganization(nextFolderId: string, nextTags: string[], nextColor: string) {
    if (!note || orgInFlight.current) {
      return;
    }
    orgInFlight.current = true;
    try {
      const summary = await setNoteOrganization(note.id, nextFolderId, nextTags, nextColor);
      const refreshed = {
        ...noteRef.current!,
        folderId: summary.folderId,
        tags: summary.tags,
        effectiveTags: summary.effectiveTags,
        color: summary.color,
        effectiveColor: summary.effectiveColor,
      };
      noteRef.current = refreshed;
      setNote(refreshed);
      setFolderId(summary.folderId);
      setOwnTags(summary.tags);
      setColor(summary.color);
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
    void persistOrganization(folderId, next, color);
  }

  function removeOwnTag(tag: string) {
    const next = ownTags.filter((t) => t !== tag);
    setOwnTags(next);
    void persistOrganization(folderId, next, color);
  }

  function onFolderChange(value: string) {
    const nextFolderId = value === "ungrouped" ? "" : value;
    setFolderId(nextFolderId);
    void persistOrganization(nextFolderId, ownTags, color);
  }

  function onColorChange(nextColor: string) {
    setColor(nextColor);
    void persistOrganization(folderId, ownTags, nextColor);
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

  useModuleAction(
    note
      ? {
          id: "notes.toggle-meta-panel",
          icon: PanelRight,
          label: "Note details",
          isActive: metaOpen,
          onClick: () => setMetaOpen((v) => !v),
        }
      : null,
  );

  useEffect(() => {
    if (!metaOpen || !note) {
      closePanel();
      return;
    }
    openPanel(
      <NoteMetaPanel
        folders={folders}
        folderId={folderId}
        ownTags={ownTags}
        color={color}
        tagDraft={tagDraft}
        inherited={inherited}
        onTagDraftChange={setTagDraft}
        onFolderChange={onFolderChange}
        onColorChange={onColorChange}
        onAddTag={tryAddTag}
        onRemoveTag={removeOwnTag}
      />,
      { width: 420, onClose: () => setMetaOpen(false) },
    );
    return () => closePanel();
    // onFolderChange/onColorChange/tryAddTag/removeOwnTag (and the onSaved
    // prop they call through to) close over state already listed below, not
    // over anything effect-relevant; omitted to avoid re-pushing panel
    // content on every render (they're new function references each
    // render, but their behavior only changes when these listed values do).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [metaOpen, note, folders, folderId, ownTags, color, tagDraft, inherited]);

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
    <div className="notes-font-scope flex h-full flex-1 flex-col">
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
        <nav
          aria-label="Note breadcrumb"
          className="flex min-w-0 items-center gap-1 text-xs text-muted-foreground"
        >
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
      {saveError ? (
        <p role="alert" className="border-b border-border px-4 py-1 text-xs text-destructive">
          {saveError}
        </p>
      ) : null}
      <Plate editor={editor} onChange={() => scheduleSave(title)}>
        <EditorContainer className="flex-1 overflow-auto">
          <div className="mx-auto w-full max-w-[720px] px-6 pt-10 pb-24">
            <Input
              aria-label="note-title"
              value={title}
              onChange={(e) => {
                titleRef.current = e.target.value;
                setTitle(e.target.value);
                scheduleSave(e.target.value);
              }}
              className="h-auto w-full border-none bg-transparent px-0 py-1 text-3xl font-bold tracking-tight shadow-none focus-visible:ring-0"
            />
            <Editor placeholder="Write something..." className="mt-6 px-0" />
          </div>
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
