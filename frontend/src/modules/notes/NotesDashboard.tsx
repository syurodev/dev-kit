import { useEffect, useMemo, useState } from "react";
import { MoreHorizontal, Plus, X } from "lucide-react";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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
} from "../../bridge/notes";
import { FOLDER_COLORS, folderDotVar, type FolderColor } from "./folderColors";
import { ColorPicker } from "./ColorPicker";
import { NoteCard } from "./NoteCard";
import { tagChipVar } from "./tagColor";

type Scope = "all" | "ungrouped" | `folder:${string}`;

type NotesDashboardProps = {
  refreshToken: number;
  onOpenNote: (id: string) => void;
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
  onChanged,
}: NotesDashboardProps) {
  const [notes, setNotes] = useState<NoteSummary[]>([]);
  const [folders, setFolders] = useState<NoteFolder[]>([]);
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState<Scope>("all");
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

  const folderIds = useMemo(() => new Set(folders.map((f) => f.id)), [folders]);

  async function refresh(q: string) {
    try {
      const [list, folderList] = await Promise.all([
        q.trim() ? searchNotes(q) : listNotes(),
        listFolders(),
      ]);
      setNotes(list);
      setFolders(folderList);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  useEffect(() => {
    void refresh(query);
    // Intentionally omits `query` — typing refreshes via onChange; this
    // effect covers mount + refreshToken bumps from editor/folder CRUD.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshToken]);

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
      onOpenNote(created.id);
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
      await refresh(query);
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
      await refresh(query);
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
      await refresh(query);
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
      await refresh(query);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  function scopeButtonClass(active: boolean) {
    return cn(
      "flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm",
      active ? "bg-accent text-accent-foreground" : "hover:bg-accent/60",
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-1">
      <aside className="flex w-56 shrink-0 flex-col border-r border-border">
        <ScrollArea className="min-h-0 flex-1">
          <div className="flex flex-col gap-0.5 p-2">
            <button
              type="button"
              className={scopeButtonClass(scope === "all")}
              onClick={() => setScope("all")}
            >
              <span className="flex-1 truncate">All</span>
              <span className="text-xs text-muted-foreground">{counts.all}</span>
            </button>
            <button
              type="button"
              className={scopeButtonClass(scope === "ungrouped")}
              onClick={() => setScope("ungrouped")}
            >
              <span className="flex-1 truncate">Ungrouped</span>
              <span className="text-xs text-muted-foreground">{counts.ungrouped}</span>
            </button>

            {folders.map((folder) => (
              <div key={folder.id} className="group relative flex items-center">
                <button
                  type="button"
                  className={cn(scopeButtonClass(scope === `folder:${folder.id}`), "pr-8")}
                  onClick={() => setScope(`folder:${folder.id}`)}
                >
                  <span
                    className="size-2 shrink-0 rounded-full"
                    style={{ background: folderDotVar(folder.color) }}
                    aria-hidden
                  />
                  <span className="flex-1 truncate">{folder.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {counts.byFolder.get(folder.id) ?? 0}
                  </span>
                </button>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="ghost"
                      className="absolute right-0.5 size-7 opacity-0 group-hover:opacity-100 data-[state=open]:opacity-100"
                      aria-label={`${folder.name} options`}
                    >
                      <MoreHorizontal className="size-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem
                      onClick={() => {
                        setRenameFolder(folder);
                        setRenameName(folder.name);
                        setRenameColor(
                          (FOLDER_COLORS.includes(folder.color as FolderColor)
                            ? folder.color
                            : "gray") as FolderColor,
                        );
                      }}
                    >
                      Rename
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() => {
                        setEditTagsFolder(folder);
                        setEditTags([...folder.tags]);
                      }}
                    >
                      Edit tags
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={() => setDeleteTarget(folder)}
                    >
                      Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            ))}
          </div>
        </ScrollArea>
        <div className="border-t border-border p-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="w-full justify-start gap-2"
            onClick={() => setCreateOpen(true)}
          >
            <Plus className="size-4" />
            New folder
          </Button>
        </div>
      </aside>

      <section className="flex min-h-0 flex-1 flex-col">
        <div className="flex flex-wrap items-center gap-2 border-b border-border p-3">
          <Input
            aria-label="note-search"
            placeholder="Search notes..."
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              void refresh(e.target.value);
            }}
            className="h-8 max-w-sm"
          />
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={creating}
            onClick={() => void onCreateNote()}
          >
            <Plus className="size-4" />
            New note
          </Button>
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

        <ScrollArea className="min-h-0 flex-1">
          <div className="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3.5 p-3">
            {filtered.map((note) => (
              <NoteCard
                key={note.id}
                note={note}
                onOpen={onOpenNote}
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
      </section>

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
