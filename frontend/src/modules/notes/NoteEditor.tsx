import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  Bold,
  Code,
  FileText,
  Folder,
  Italic,
  Link2,
  Lock,
  Palette,
  PanelRight,
  History,
  Strikethrough,
  Tag,
  Underline,
  X,
} from "lucide-react";
import { KEYS } from "platejs";
import { Plate, usePlateEditor } from "platejs/react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Editor, EditorContainer } from "@/components/ui/editor";
import { FloatingToolbar } from "@/components/ui/floating-toolbar";
import { LinkToolbarButton } from "@/components/ui/link-toolbar-button";
import { MarkToolbarButton } from "@/components/ui/mark-toolbar-button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useSplitPanelActions } from "@/components/ui/split-panel";
import { errorMessage } from "../../bridge/errors";
import {
  createNote,
  deleteNote,
  getNote,
  listFolders,
  setNoteOrganization,
  updateNote,
  type Note,
  type NoteFolder,
  listBacklinks,
  checkpointNote,
  restoreNote,
} from "../../bridge/notes";
import { useModuleAction } from "../../stores/moduleActions";
import { NoteHistoryPanel } from "./NoteHistoryPanel";
import { NoteOptionsMenu } from "./NoteOptionsMenu";
import { useNotesSidebar } from "./useNotesSidebar";
import { useNotesRefreshStore } from "../../stores/notesRefresh";
import { useNoteViewStore } from "../../stores/noteView";
import { copyText } from "../../bridge/clipboard";
import { openNoteTab } from "./documents";
import { tabKey, useWorkbenchTabsStore } from "../../stores/workbenchTabs";
import { ColorPicker } from "./ColorPicker";
import { folderDotVar } from "./folderColors";
import { tagChipVar } from "./tagColor";
import { NotesEditorKit } from "./plate/plugins";
import {
  NOTE_CRUMB_CLASS,
  NoteTitleField,
  type NoteView,
  noteBodyClass,
  notePageClass,
  noteTitleClass,
} from "./noteLayout";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";

type SaveStatus = "idle" | "saving" | "saved" | "error";

type NoteEditorProps = {
  params?: Record<string, unknown>;
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
    // Tag / Chip (378:2674): state-soft on a subtle stroke, radius 6, the
    // name in 11 Medium and a 10px remove glyph.
    <span className="inline-flex items-center gap-1.25 rounded-sm border border-border-subtle bg-state-soft py-0.75 pr-1.5 pl-2 text-caption-11-medium text-text-secondary">
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
          className="inline-flex text-text-tertiary hover:text-text-primary"
          onClick={onRemove}
        >
          <X className="size-2.5" />
        </button>
      ) : null}
    </span>
  );
}

function NoteBacklinks({ noteId }: { noteId: string }) {
  const { data: backlinks = EMPTY_ARRAY } = useQuery({
    queryKey: ["notes", "backlinks", noteId],
    queryFn: () => listBacklinks(noteId),
    enabled: Boolean(noteId),
  });

  if (backlinks.length === 0) return null;

  return (
    <div className="flex flex-col gap-4.5">
      <MetaLabel icon={Link2}>Backlinks · {backlinks.length}</MetaLabel>
      <ul className="-mt-2 flex flex-col gap-2">
        {backlinks.map((note) => (
          <li key={note.id} className="flex flex-col gap-0.75 rounded-control bg-state-soft px-3 py-2.25">
            <span className="flex items-center gap-1.5">
              <Link2 className="size-2.75 shrink-0 text-text-tertiary" />
              <span className="truncate text-label-12 text-text-primary">{note.title || "Untitled"}</span>
            </span>
            <p className="truncate text-caption-11 text-text-tertiary">{note.preview}</p>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Meta panel `label-row` (445:1225): a 12px glyph beside a caps label. */
function MetaLabel({ icon: Icon, children }: { icon: typeof Folder; children: React.ReactNode }) {
  return (
    <span className="flex items-center gap-1.75 text-label-11-caps text-text-tertiary">
      <Icon className="size-3" />
      {children}
    </span>
  );
}

/**
 * Figma `Notes / Meta Panel Content` (424:3542): "Note details", then FOLDER
 * (a select), COLOR (dots + "Inherit from folder"), TAGS (a chip field with
 * the inherited line under it) and BACKLINKS · n as soft cards. 20px inset,
 * 18px between groups.
 */
function NoteMetaPanel({
  noteId,
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
  noteId: string;
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
    <div className="flex flex-col gap-4.5 p-5">
      <span className="text-body-14-medium text-text-primary">Note details</span>
      <div className="flex flex-col gap-4.5">
        <MetaLabel icon={Folder}>Folder</MetaLabel>
        <Select value={folderId || "ungrouped"} onValueChange={onFolderChange}>
          <SelectTrigger className="w-full border-border bg-state-soft" aria-label="note-folder">
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

      <div className="flex flex-col gap-4.5">
        <MetaLabel icon={Palette}>Color</MetaLabel>
        <ColorPicker value={color} onChange={onColorChange} allowInherit />
      </div>

      <div className="flex flex-col gap-4.5">
        <MetaLabel icon={Tag}>Tags</MetaLabel>
        {/* Tag / Input (378:2678): chips and the caret share one field. */}
        <label className="flex min-h-9 w-full flex-wrap items-center gap-1.5 rounded-md border border-border bg-state-soft py-1.5 pr-2.5 pl-2">
          {ownTags.map((tag) => (
            <TagChip key={tag} tag={tag} onRemove={() => onRemoveTag(tag)} />
          ))}
          <input
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
            className="min-w-16 flex-1 bg-transparent text-body-12 text-text-primary outline-none placeholder:text-text-tertiary"
          />
        </label>
        {inherited.length > 0 ? (
          <span className="-mt-2 truncate text-code-11 text-text-tertiary">
            inherited from folder:  {inherited.join("  ")}
          </span>
        ) : null}
      </div>

      <NoteBacklinks noteId={noteId} />
    </div>
  );
}

export function NoteEditor({ params }: NoteEditorProps) {
  const noteId = typeof params?.noteId === "string" ? params.noteId : "";
  const tabId = useMemo(() => tabKey("notes", "notes.editor", { noteId }), [noteId]);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);
  const closeTab = useWorkbenchTabsStore((state) => state.closeTab);
  const pinTab = useWorkbenchTabsStore((state) => state.pinTab);
  const bumpNotesRefresh = useNotesRefreshStore((state) => state.bump);
  const fullWidthNotes = useNoteViewStore((state) => state.fullWidth);
  const smallTextNotes = useNoteViewStore((state) => state.smallText);
  const lockedNotes = useNoteViewStore((state) => state.locked);
  const noteFont = useNoteViewStore((state) => state.fonts[noteId] ?? "default");
  const toggleFullWidth = useNoteViewStore((state) => state.toggleFullWidth);
  const toggleSmallText = useNoteViewStore((state) => state.toggleSmallText);
  const toggleLocked = useNoteViewStore((state) => state.toggleLocked);
  const setNoteFont = useNoteViewStore((state) => state.setFont);
  const isFullWidth = fullWidthNotes.includes(noteId);
  const isLocked = lockedNotes.includes(noteId);
  const view: NoteView = {
    fullWidth: isFullWidth,
    smallText: smallTextNotes.includes(noteId),
    font: noteFont,
    locked: isLocked,
  };
  const hasEditedRef = useRef(false);

  function onBack() {
    openTab({ moduleId: "notes", viewId: "notes.list", title: "Notes" });
  }

  function onDeleted() {
    closeTab(tabId, { remember: false });
    bumpNotesRefresh();
  }

  function onSaved() {
    bumpNotesRefresh();
  }

  const [note, setNote] = useState<Note | null>(null);
  const [folders, setFolders] = useState<NoteFolder[]>([]);
  const [title, setTitle] = useState("");
  const [folderId, setFolderId] = useState("");
  const [ownTags, setOwnTags] = useState<string[]>([]);
  const [color, setColor] = useState("");
  const [tagDraft, setTagDraft] = useState("");
  const [metaOpen, setMetaOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
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

  const { openPanel, closePanel } = useSplitPanelActions();

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
        // Doesn't trip scheduleSave's pin-on-first-edit guard: this whole
        // .then() runs in one synchronous batch before `note` is set, so
        // <Plate>/<Slate> (gated on `note` below) hasn't mounted yet and
        // slate-react hasn't registered its onChange listener — setValue's
        // operations fire with nobody listening. Depends on that mount
        // ordering; don't move this setValue after <Plate> can be mounted
        // (e.g. behind a loading overlay instead of an early return)
        // without re-checking this still holds.
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
  }, [noteId]);

  function scheduleSave(nextTitle: string) {
    if (!hasEditedRef.current) {
      hasEditedRef.current = true;
      pinTab(tabId);
    }
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

  function toggleHistory() {
    setHistoryOpen((open) => {
      // One split panel, two things that want it. Opening history puts the
      // details panel away rather than fighting over the slot.
      if (!open) setMetaOpen(false);
      return !open;
    });
  }

  async function onCopyContents() {
    try {
      await copyText(editor.api.markdown.serialize());
      toast.success("Copied as Markdown");
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  /** Notion's Duplicate: the text as it is on screen (unsaved edits too), the
   *  folder, tags and colour; opened beside nothing, as a document of its own. */
  async function onDuplicate() {
    if (!note) return;
    try {
      const copy = await createNote(
        `${titleRef.current.trim() || "Untitled"} (copy)`,
        editor.api.markdown.serialize(),
        folderId,
        ownTags,
      );
      if (color) await setNoteOrganization(copy.id, folderId, ownTags, color);
      bumpNotesRefresh();
      openNoteTab(copy, false);
    } catch (e) {
      toast.error(errorMessage(e));
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

  // The rail belongs to the module, not to the dashboard. Registering it here
  // too is what stops it emptying the moment a note is opened; picking a scope
  // from inside the editor goes back to the list, where that scope means
  // something.
  useNotesSidebar(onBack);

  useModuleAction(
    note
      ? {
          id: "notes.toggle-history-panel",
          icon: History,
          label: "History",
          isActive: historyOpen,
          onClick: toggleHistory,
        }
      : null,
  );

  // Checkpoint on idle, not on every keystroke: a snapshot per character would
  // blow through the fifty-version cap in a paragraph. Checkpoint is a no-op
  // when nothing changed, so a timer that fires on an untouched note costs a
  // round trip and writes nothing.
  useEffect(() => {
    if (!note) return;
    const timer = window.setInterval(
      () => void checkpointNote(note.id).catch(() => undefined),
      5 * 60 * 1000,
    );
    return () => window.clearInterval(timer);
  }, [note?.id]);

  useEffect(() => {
    if (!historyOpen || !note) return;
    openPanel(
      <NoteHistoryPanel
        noteId={note.id}
        currentBody={editor.api.markdown.serialize()}
        onRestored={(body) => {
          const value = editor.api.markdown.deserialize(body);
          editor.tf.setValue(value);
        }}
      />,
      { width: 460, onClose: () => setHistoryOpen(false), key: `history:${note.id}` },
    );
    // Discarded when it closes, not kept hidden like Metadata: its versions
    // list and the body it would restore over must be read fresh each time.
    return () => closePanel({ discard: true });
  }, [historyOpen, note?.id]);

  useEffect(() => {
    if (historyOpen) return;
    if (!metaOpen || !note) {
      closePanel();
      return;
    }
    openPanel(
      <NoteMetaPanel
        noteId={note.id}
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
      { width: 420, onClose: () => setMetaOpen(false), key: `meta:${note.id}` },
    );
    return () => closePanel();
    // onFolderChange/onColorChange/tryAddTag/removeOwnTag (and the onSaved
    // prop they call through to) close over state already listed below, not
    // over anything effect-relevant; omitted to avoid re-pushing panel
    // content on every render (they're new function references each
    // render, but their behavior only changes when these listed values do).
  }, [metaOpen, note, folders, folderId, ownTags, color, tagDraft, inherited]);

  if (loadError) {
    return (
      <div className="flex flex-1 items-center justify-center p-4">
        <p role="alert" className="text-body-13 text-destructive">
          {loadError}
        </p>
      </div>
    );
  }

  if (!note) {
    return (
      <div className="flex flex-1 items-center justify-center p-4">
        <p className="text-body-13 text-muted-foreground">Loading…</p>
      </div>
    );
  }

  return (
    <div className="notes-font-scope flex h-full min-h-0 flex-1 flex-col overflow-hidden">
      {/* Figma `Notes / 03b Editor — Note trashed elsewhere`. The tab stays
          open rather than closing under the reader: the body is on screen and
          still theirs to copy out. Saying so beats a tab that vanishes. */}
      {note.deletedAt && (
        <div className="flex shrink-0 items-center justify-between gap-3 border-b border-status-warning/30 bg-status-warning/10 px-4 py-2 text-body-12 text-status-warning">
          <span>
            <strong className="font-semibold">This note is in the trash.</strong> It was deleted
            somewhere else while you had it open. Edits still save, and it stays in the trash
            until it is restored or purged.
          </span>
          <Button
            size="sm"
            variant="outline"
            className="h-7 shrink-0 text-label-12"
            onClick={() => void restoreNote(note.id).then(() => setNote({ ...note, deletedAt: "" }))}
          >
            Restore
          </Button>
        </div>
      )}
      {saveError ? (
        <p
          role="alert"
          className="shrink-0 border-b border-border-subtle px-6 py-1 text-body-12 text-status-danger"
        >
          {saveError}
        </p>
      ) : null}
      <Plate editor={editor} readOnly={isLocked} onChange={() => scheduleSave(title)}>
        <EditorContainer className="min-h-0 flex-1 overflow-y-auto">
          {/* Editor Content (424:3473): the crumb sits on the writing column's
              own left edge, 20 down; 720 wide. No header bar — the tab strip
              above is the header. The ⋯ menu's choices (noteView.ts) are
              Notion's per-page Appearance: full width drops that 720px
              ceiling. The column itself is drawn per block (noteLayout.tsx), so
              tables can use the pane's width; the crumb spans it too. */}
          <div className={notePageClass(view.font)}>
            <div className={NOTE_CRUMB_CLASS}>
              <button
                type="button"
                aria-label="Back to notes"
                className="shrink-0 hover:text-text-primary"
                onClick={onBack}
              >
                <ArrowLeft className="size-3.25" />
              </button>
              <FileText className="size-3.25 shrink-0" />
              <nav aria-label="Note breadcrumb" className="flex min-w-0 items-center gap-1">
                <button type="button" className="shrink-0 hover:text-text-primary" onClick={onBack}>
                  Notes
                </button>
                <span aria-hidden>/</span>
                <span className="truncate">{folderName}</span>
                <span aria-hidden>/</span>
                <span className="truncate text-text-secondary">{title || "Untitled"}</span>
              </nav>
              <span className="ml-auto shrink-0">
                {status === "saving" ? "Saving…" : status === "saved" ? "Saved" : null}
              </span>
              {isLocked && (
                <button
                  type="button"
                  title="Unlock to edit"
                  className="flex shrink-0 items-center gap-1 hover:text-text-primary"
                  onClick={() => toggleLocked(noteId)}
                >
                  <Lock className="size-3.25" />
                  Locked
                </button>
              )}
              <NoteOptionsMenu
                view={view}
                folders={folders}
                folderId={folderId}
                historyOpen={historyOpen}
                onFontChange={(font) => setNoteFont(noteId, font)}
                onToggleSmallText={() => toggleSmallText(noteId)}
                onToggleFullWidth={() => toggleFullWidth(noteId)}
                onToggleLocked={() => toggleLocked(noteId)}
                onCopyContents={() => void onCopyContents()}
                onDuplicate={() => void onDuplicate()}
                onMoveTo={(next) => onFolderChange(next || "ungrouped")}
                onToggleHistory={toggleHistory}
                onDelete={() => void onDelete()}
              />
            </div>
            <NoteTitleField
              value={title}
              className={noteTitleClass(isFullWidth)}
              readOnly={isLocked}
              onChange={(next) => {
                titleRef.current = next;
                setTitle(next);
                scheduleSave(next);
              }}
              onEnter={() => editor.tf.focus({ edge: "start" })}
            />
            <Editor variant="none" placeholder="Write something..." className={noteBodyClass(view)} />
          </div>
          {!isLocked && (
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
          )}
        </EditorContainer>
      </Plate>
    </div>
  );
}
