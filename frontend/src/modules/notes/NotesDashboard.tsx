import { useEffect, useMemo, useState } from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Search, X } from "lucide-react";
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
import { Empty, EmptyDescription } from "@/components/ui/empty";
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
} from "../../bridge/notes";
import { noteColorValue } from "./folderColors";
import { ColorPicker } from "./ColorPicker";
import { useNotesSidebar } from "./useNotesSidebar";
import { invalidateNotesOnce } from "./railData";
import { useNotesSidebarStore } from "./notesSidebarStore";
import { NotesTrash } from "./NotesTrash";
import { NoteCard } from "./NoteCard";
import { tagChipVar } from "./tagColor";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";

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
  const [cardsRef] = useAutoAnimate<HTMLDivElement>({ duration: 180, easing: "cubic-bezier(0.23, 1, 0.32, 1)" });
  const queryClient = useQueryClient();
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const scope = useNotesSidebarStore((s) => s.scope) as Scope;
  const setScope = useNotesSidebarStore((s) => s.setScope);
  const [selectedTags, setSelectedTags] = useState<string[]>([]);
  const [creating, setCreating] = useState(false);

  const [createOpen, setCreateOpen] = useState(false);
  const [createName, setCreateName] = useState("");
  const [createColor, setCreateColor] = useState<string>("blue");
  const [createTags, setCreateTags] = useState<string[]>([]);

  const [renameFolder, setRenameFolder] = useState<NoteFolder | null>(null);
  const [renameName, setRenameName] = useState("");
  const [renameColor, setRenameColor] = useState<string>("blue");

  const [editTagsFolder, setEditTagsFolder] = useState<NoteFolder | null>(null);
  const [editTags, setEditTags] = useState<string[]>([]);

  const [deleteTarget, setDeleteTarget] = useState<NoteFolder | null>(null);

  useEffect(() => {
    const timer = window.setTimeout(
      () => setDebouncedQuery(query.trim()),
      query.trim() ? 200 : 0,
    );
    return () => window.clearTimeout(timer);
  }, [query]);

  const { data: notes = EMPTY_ARRAY } = useQuery({
    queryKey: ["notes", "list", debouncedQuery],
    queryFn: () => (debouncedQuery ? searchNotes(debouncedQuery) : listNotes()),
    placeholderData: (prev) => prev,
  });

  const { data: folders = EMPTY_ARRAY } = useQuery({
    queryKey: ["notes", "folders"],
    queryFn: () => listFolders(),
  });

  // The trash count and the rail's lists are loaded by the rail itself
  // (railData.ts), which this view mounts through useNotesSidebar.
  useEffect(() => {
    invalidateNotesOnce(queryClient, refreshToken);
  }, [refreshToken, queryClient]);

  const folderIds = useMemo(() => new Set(folders.map((f) => f.id)), [folders]);

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


  // Every tag in play, as toggle chips beside the search (Figma 799:3174 shows
  // the whole set with the active ones lit), not only the selected ones.
  const allTags = useMemo(() => {
    const seen = new Map<string, string>();
    for (const n of notes) {
      for (const tag of n.effectiveTags) {
        const key = tag.toLowerCase();
        if (!seen.has(key)) seen.set(key, tag);
      }
    }
    return [...seen.values()].sort((a, b) => a.localeCompare(b));
  }, [notes]);

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

  const pendingAction = useNotesSidebarStore((s) => s.pendingAction);
  const clearAction = useNotesSidebarStore((s) => s.clearAction);

  // The dialogs live here, so a folder action raised from the editor's copy of
  // the rail is handed over rather than duplicated.
  useEffect(() => {
    if (!pendingAction) return;
    if (pendingAction.kind === "create") {
      setCreateOpen(true);
    } else if (pendingAction.kind === "newNote") {
      void onCreateNote();
    } else if (pendingAction.kind === "rename") {
      setRenameFolder(pendingAction.folder);
      setRenameName(pendingAction.folder.name);
      setRenameColor(noteColorValue(pendingAction.folder.color) ? pendingAction.folder.color : "gray");
    } else if (pendingAction.kind === "tags") {
      setEditTagsFolder(pendingAction.folder);
      setEditTags([...pendingAction.folder.tags]);
    } else {
      setDeleteTarget(pendingAction.folder);
    }
    clearAction();
  }, [pendingAction, clearAction]);

  useNotesSidebar();

  return (
    <div className="notes-font-scope contain-layout flex h-full min-h-0 flex-1">
      <div className="flex h-full min-h-0 w-full">

        <section className="flex min-h-0 flex-1 flex-col">
          {/* Search and "New note" on one row, the tag filter on its own
              row under them. Figma (799:3369) draws all three on one row,
              which holds for four short tags; with more, the chips wrapped
              into a tall block squeezed between the search box and the
              button, and the cards started well down the page. No page
              title: the rail and the tab already say where you are. */}
          <div className="flex flex-col gap-3 px-6 pt-5 pb-5">
            <div className="flex items-center gap-3">
              <div className="relative max-w-120 min-w-0 flex-1">
                <Search className="pointer-events-none absolute top-1/2 left-3 size-3.5 -translate-y-1/2 text-text-tertiary" />
                <Input
                  aria-label="note-search"
                  placeholder="Search notes…"
                  value={query}
                  onChange={(e) => {
                    setQuery(e.target.value);
                  }}
                  className="h-8 border-border-subtle bg-surface-well pl-8"
                />
              </div>
              <div className="flex-1" />
              <Button
                type="button"
                size="default"
                disabled={creating}
                onClick={() => void onCreateNote()}
              >
                <Plus className="size-3.5" />
                New note
              </Button>
            </div>
            {allTags.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5" aria-label="Filter by tag">
                {allTags.map((tag) => {
                  const key = tag.toLowerCase();
                  const active = selectedTags.some((t) => t.toLowerCase() === key);
                  return (
                    <button
                      key={key}
                      type="button"
                      aria-pressed={active}
                      onClick={() => toggleTagFilter(tag)}
                      className={cn(
                        "flex h-6 items-center gap-1.5 rounded-md border px-2 text-code-11 transition-colors",
                        active
                          ? "border-border bg-state-soft text-text-primary"
                          : "border-border-subtle bg-surface-raised text-text-secondary hover:text-text-primary",
                      )}
                    >
                      <span
                        className="size-1.5 shrink-0 rounded-full"
                        style={{ background: tagChipVar(tag) }}
                        aria-hidden
                      />
                      {tag}
                    </button>
                  );
                })}
                {selectedTags.length > 0 ? (
                  <button
                    type="button"
                    onClick={() => setSelectedTags([])}
                    className="flex h-6 items-center gap-1 rounded-md px-2 text-code-11 text-text-tertiary hover:text-text-primary"
                  >
                    <X className="size-3" />
                    Clear
                  </button>
                ) : null}
              </div>
            ) : null}
          </div>

          {scope === "trash" ? (
            // onChanged bumps the refresh token, and the rail re-reads the
            // trash count from that like everything else it lists.
            <NotesTrash onChanged={onChanged} />
          ) : (
          <ScrollArea className="min-h-0 flex-1">
            {/* 348-wide cards on a 20px gap (Figma draws 22/20 — one step on
                the 4px grid), 24px in from the card edge. */}
            <div ref={cardsRef} className="grid grid-cols-[repeat(auto-fill,minmax(348px,1fr))] gap-5 px-6 pb-6">
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
                <Empty className="col-span-full px-1 py-6">
                  <EmptyDescription>No notes match this view.</EmptyDescription>
                </Empty>
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
