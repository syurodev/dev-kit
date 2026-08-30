import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Plus,
  X,
} from "lucide-react";
import { toast } from "sonner";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { errorMessage } from "../../bridge/errors";
import {
  createFolder,
  createNote,
  deleteFolder,
  listFolders,
  listNotes,
  searchNotes,
  updateFolder,
  type NoteFolder,
  type NoteSummary,
  listTrashedNotes,
} from "../../bridge/notes";
import { FOLDER_COLORS, type FolderColor } from "./folderColors";
import { ColorPicker } from "./ColorPicker";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { NotesSidebar } from "./NotesSidebar";
import { NotesTrash } from "./NotesTrash";
import { NoteCard } from "./NoteCard";
import { tagChipVar } from "./tagColor";

type Scope = "all" | "ungrouped" | "trash" | `folder:${string}`;

type NotesDashboardProps = {
  refreshToken: number;
  onOpenNote: (note: { id: string; title: string }) => void;
  onOpenNoteDouble: (note: { id: string; title: string }) => void;
  onPreloadEditor?: () => void;
  onChanged: () => void;
};

function parseTags(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of raw.split(/[,]+/)) {
    const tag = part.trim();
    if (!tag) continue;
    const key = tag.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(tag);
  }
  return out;
}

function TagChip({
  tag,
  onClick,
  onRemove,
}: {
  tag: string;
  onClick?: () => void;
  onRemove?: () => void;
}) {
  return (
    <Badge
      variant="secondary"
      className={cn(
        "gap-1.5 bg-muted text-foreground",
        onClick && "cursor-pointer",
      )}
      onClick={
        onClick
          ? (e) => {
              e.stopPropagation();
              onClick();
            }
          : undefined
      }
    >
      <span
        className="size-1.5 shrink-0 rounded-full"
        style={{ background: tagChipVar(tag) }}
        aria-hidden
      />
      {tag}
      {onRemove ? (
        <span
          role="button"
          tabIndex={0}
          aria-label={`Remove ${tag} filter`}
          className="inline-flex"
          onClick={(e) => {
            e.stopPropagation();
            onRemove();
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              e.stopPropagation();
              onRemove();
            }
          }}
        >
          <X className="size-3" />
        </span>
      ) : null}
    </Badge>
  );
}

function TagsEditor({
  tags,
  onChange,
}: {
  tags: string[];
  onChange: (tags: string[]) => void;
}) {
  const [draft, setDraft] = useState("");

  function addFromDraft() {
    const next = parseTags(draft);
    if (next.length === 0) return;
    const merged = [...tags];
    const seen = new Set(tags.map((t) => t.toLowerCase()));
    for (const tag of next) {
      const key = tag.toLowerCase();
      if (seen.has(key)) continue;
      seen.add(key);
      merged.push(tag);
    }
    onChange(merged);
    setDraft("");
  }

  return (
    <div className="space-y-2">
      <Input
        aria-label="folder-tags"
        placeholder="Add tags (comma-separated, Enter)"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            addFromDraft();
          }
        }}
      />
      {tags.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {tags.map((tag) => (
            <TagChip
              key={tag.toLowerCase()}
              tag={tag}
              onRemove={() =>
                onChange(tags.filter((t) => t.toLowerCase() !== tag.toLowerCase()))
              }
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

export function NotesDashboard({
  refreshToken,
  onOpenNote,
  onOpenNoteDouble,
  onPreloadEditor,
  onChanged,
}: NotesDashboardProps) {
  const [notes, setNotes] = useState<NoteSummary[]>([]);
  const [folders, setFolders] = useState<NoteFolder[]>([]);
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState<Scope>("all");
  // The badge is read from the trash itself rather than derived from the note
  // list, because List has already filtered trashed notes out by the time it
  // gets here — counting them from it would always report zero.
  const [trashCount, setTrashCount] = useState(0);
  const [foldersOpen, setFoldersOpen] = useState(true);
  const [selectedTags, setSelectedTags] = useState<string[]>([]);
  const [creating, setCreating] = useState(false);

  const [createOpen, setCreateOpen] = useState(false);
  const [createName, setCreateName] = useState("");
  const [createColor, setCreateColor] = useState<FolderColor>("blue");
  const [createTags, setCreateTags] = useState<string[]>([]);

  const [renameFolder, setRenameFolder] = useState<NoteFolder | null>(null);
  const [renameName, setRenameName] = useState("");
  const [renameColor, setRenameColor] = useState<FolderColor>("blue");

  const [editTagsFolder, setEditTagsFolder] = useState<NoteFolder | null>(null);
  const [editTags, setEditTags] = useState<string[]>([]);

  const [deleteTarget, setDeleteTarget] = useState<NoteFolder | null>(null);
  const refreshRequestRef = useRef(0);

  const folderIds = useMemo(() => new Set(folders.map((f) => f.id)), [folders]);

  const refresh = useCallback(async (q: string) => {
    const request = ++refreshRequestRef.current;
    try {
      const [list, folderList] = await Promise.all([
        q.trim() ? searchNotes(q) : listNotes(),
        listFolders(),
      ]);
      // A slower bridge response for an older query must not replace the
      // results for what the user is currently typing.
      if (request !== refreshRequestRef.current) return;
      setNotes(list);
      setFolders(folderList);
    } catch (e) {
      if (request === refreshRequestRef.current) {
        toast.error(errorMessage(e));
      }
    }
  }, []);

  useEffect(() => {
    void listTrashedNotes()
      .then((items) => setTrashCount(items.length))
      .catch(() => setTrashCount(0));
  }, [refreshToken, scope]);

  useEffect(() => {
    // Searching through the native bridge on every keystroke creates avoidable
    // IPC and SQLite work. Empty queries and explicit refreshes remain fast;
    // typed searches are coalesced into one request after a short pause.
    const timer = window.setTimeout(
      () => void refresh(query),
      query.trim() ? 200 : 0,
    );
    return () => {
      window.clearTimeout(timer);
      // Also invalidate a request that already crossed the debounce boundary.
      refreshRequestRef.current += 1;
    };
  }, [query, refresh, refreshToken]);

  const filtered = useMemo(() => {
    return notes.filter((n) => {
      if (scope === "ungrouped") {
        if (n.folderId && folderIds.has(n.folderId)) return false;
      } else if (scope.startsWith("folder:")) {
        if (n.folderId !== scope.slice(7)) return false;
      }
      return selectedTags.every((tag) =>
        n.effectiveTags.some((t) => t.toLowerCase() === tag.toLowerCase()),
      );
    });
  }, [notes, scope, folderIds, selectedTags]);

  const counts = useMemo(() => {
    let ungrouped = 0;
    const byFolder = new Map<string, number>();
    for (const n of notes) {
      if (!n.folderId || !folderIds.has(n.folderId)) {
        ungrouped += 1;
      } else {
        byFolder.set(n.folderId, (byFolder.get(n.folderId) ?? 0) + 1);
      }
    }
    return { all: notes.length, ungrouped, byFolder };
  }, [notes, folderIds]);

  const scopeLabel = useMemo(() => {
    if (scope === "all") return "All";
    if (scope === "ungrouped") return "Ungrouped";
    const folderId = scope.slice(7);
    return folders.find((f) => f.id === folderId)?.name ?? "All";
  }, [scope, folders]);

  function toggleTagFilter(tag: string) {
    setSelectedTags((prev) => {
      const key = tag.toLowerCase();
      const exists = prev.some((t) => t.toLowerCase() === key);
      if (exists) {
        return prev.filter((t) => t.toLowerCase() !== key);
      }
      return [...prev, tag];
    });
  }

  async function onCreateNote() {
    setCreating(true);
    try {
      const folderId = scope.startsWith("folder:") ? scope.slice(7) : "";
      const created = await createNote("Untitled", "", folderId, []);
      onChanged();
      // Explicit creation is a deliberate action (same as a double-click),
      // not casual browsing — opens pinned, not as a replaceable preview.
      onOpenNoteDouble(created);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setCreating(false);
    }
  }

  async function onCreateFolder() {
    const name = createName.trim();
    if (!name) return;
    try {
      await createFolder(name, createColor, createTags);
      setCreateOpen(false);
      setCreateName("");
      setCreateColor("blue");
      setCreateTags([]);
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function onSaveRename() {
    if (!renameFolder) return;
    const name = renameName.trim();
    if (!name) return;
    try {
      await updateFolder(renameFolder.id, name, renameColor, renameFolder.tags);
      setRenameFolder(null);
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function onSaveEditTags() {
    if (!editTagsFolder) return;
    try {
      await updateFolder(
        editTagsFolder.id,
        editTagsFolder.name,
        editTagsFolder.color,
        editTags,
      );
      setEditTagsFolder(null);
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function onConfirmDelete() {
    if (!deleteTarget) return;
    try {
      await deleteFolder(deleteTarget.id);
      if (scope === `folder:${deleteTarget.id}`) {
        setScope("all");
      }
      setDeleteTarget(null);
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  // The shell owns the rail — its width, its resize handle, its visibility.
  // A module registers content and never lays the rail out itself.
  useSidebarContent(
    <NotesSidebar
      scope={scope}
      onScopeChange={setScope}
      folders={folders}
      foldersOpen={foldersOpen}
      onFoldersOpenChange={setFoldersOpen}
      counts={counts}
      trashCount={trashCount}
      onCreateFolder={() => setCreateOpen(true)}
      onRenameFolder={(folder) => {
        setRenameFolder(folder);
        setRenameName(folder.name);
        setRenameColor(
          (FOLDER_COLORS.includes(folder.color as FolderColor)
            ? folder.color
            : "gray") as FolderColor,
        );
      }}
      onEditFolderTags={(folder) => {
        setEditTagsFolder(folder);
        setEditTags([...folder.tags]);
      }}
      onDeleteFolder={setDeleteTarget}
    />,
  );

  return (
    <div className="notes-font-scope contain-layout flex h-full min-h-0 flex-1">
      <div className="flex h-full min-h-0 w-full">

        <section className="flex min-h-0 flex-1 flex-col">
          <div className="flex flex-col gap-2 border-b border-border px-3 py-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-2">
                <div>
                  <h1 className="text-2xl font-bold tracking-tight text-foreground">
                    Notes
                  </h1>
                  <p className="text-[13px] text-muted-foreground">
                    {filtered.length} {filtered.length === 1 ? "note" : "notes"} ·{" "}
                    {scopeLabel}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Input
                  aria-label="note-search"
                  placeholder="Search notes..."
                  value={query}
                  onChange={(e) => {
                    setQuery(e.target.value);
                  }}
                  className="h-9 w-64"
                />
                <Button
                  type="button"
                  size="lg"
                  variant="outline"
                  disabled={creating}
                  onClick={() => void onCreateNote()}
                >
                  <Plus className="size-4" />
                  New note
                </Button>
              </div>
            </div>
            {selectedTags.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5">
                {selectedTags.map((tag) => (
                  <TagChip
                    key={tag.toLowerCase()}
                    tag={tag}
                    onClick={() => toggleTagFilter(tag)}
                    onRemove={() => toggleTagFilter(tag)}
                  />
                ))}
              </div>
            ) : null}
          </div>

          {scope === "trash" ? (
            <NotesTrash
              onChanged={() => {
                onChanged();
                void listTrashedNotes()
                  .then((items) => setTrashCount(items.length))
                  .catch(() => undefined);
              }}
            />
          ) : (
          <ScrollArea className="min-h-0 flex-1">
            <div className="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-4 p-3">
              {filtered.map((note) => (
                <NoteCard
                  key={note.id}
                  note={note}
                  onOpen={onOpenNote}
                  onOpenDouble={onOpenNoteDouble}
                  onPreload={onPreloadEditor}
                  onTagClick={toggleTagFilter}
                />
              ))}
              {filtered.length === 0 ? (
                <p className="col-span-full px-1 py-6 text-sm text-muted-foreground">
                  No notes match this view.
                </p>
              ) : null}
            </div>
          </ScrollArea>
          )}
        </section>
      </div>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>New folder</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <Input
              aria-label="folder-name"
              placeholder="Folder name"
              value={createName}
              onChange={(e) => setCreateName(e.target.value)}
            />
            <ColorPicker value={createColor} onChange={(c) => c && setCreateColor(c)} />
            <TagsEditor tags={createTags} onChange={setCreateTags} />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setCreateOpen(false)}>
              Cancel
            </Button>
            <Button
              type="button"
              disabled={!createName.trim()}
              onClick={() => void onCreateFolder()}
            >
              Create
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!renameFolder}
        onOpenChange={(open) => {
          if (!open) setRenameFolder(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Rename folder</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <Input
              aria-label="rename-folder-name"
              value={renameName}
              onChange={(e) => setRenameName(e.target.value)}
            />
            <ColorPicker value={renameColor} onChange={(c) => c && setRenameColor(c)} />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setRenameFolder(null)}>
              Cancel
            </Button>
            <Button
              type="button"
              disabled={!renameName.trim()}
              onClick={() => void onSaveRename()}
            >
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!editTagsFolder}
        onOpenChange={(open) => {
          if (!open) setEditTagsFolder(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Edit folder tags</DialogTitle>
          </DialogHeader>
          <TagsEditor tags={editTags} onChange={setEditTags} />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setEditTagsFolder(null)}>
              Cancel
            </Button>
            <Button type="button" onClick={() => void onSaveEditTags()}>
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete folder?</AlertDialogTitle>
            <AlertDialogDescription>
              Notes in this folder are kept and become ungrouped. The folder&apos;s tags are
              copied onto those notes so their effective tags stay the same.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void onConfirmDelete()}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
