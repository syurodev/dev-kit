import { useEffect, useMemo, useState } from "react";
import {
  ChevronRight,
  Folder,
  Inbox,
  LayoutGrid,
  MoreHorizontal,
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
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
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
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar";
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

  return (
    <div className="notes-font-scope contain-layout flex h-full min-h-0 flex-1">
      <SidebarProvider className="flex h-full min-h-0 w-full">
        <Sidebar collapsible="icon" className="border-r border-border">
          <SidebarContent>
            <SidebarGroup>
              <SidebarGroupContent>
                <SidebarMenu>
                  <SidebarMenuItem>
                    <SidebarMenuButton
                      isActive={scope === "all"}
                      tooltip="All"
                      onClick={() => setScope("all")}
                    >
                      <LayoutGrid />
                      <span>All</span>
                    </SidebarMenuButton>
                    <SidebarMenuBadge>{counts.all}</SidebarMenuBadge>
                  </SidebarMenuItem>
                  <SidebarMenuItem>
                    <SidebarMenuButton
                      isActive={scope === "ungrouped"}
                      tooltip="Ungrouped"
                      onClick={() => setScope("ungrouped")}
                    >
                      <Inbox />
                      <span>Ungrouped</span>
                    </SidebarMenuButton>
                    <SidebarMenuBadge>{counts.ungrouped}</SidebarMenuBadge>
                  </SidebarMenuItem>
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>

            <Collapsible defaultOpen className="group/folders">
              <SidebarGroup>
                <CollapsibleTrigger asChild>
                  <SidebarGroupLabel className="cursor-pointer">
                    <ChevronRight className="size-3 shrink-0 transition-transform group-data-[state=open]/folders:rotate-90" />
                    <span className="flex-1">Folders</span>
                  </SidebarGroupLabel>
                </CollapsibleTrigger>
                <SidebarGroupAction
                  onClick={() => setCreateOpen(true)}
                  aria-label="New folder"
                  className="opacity-0 group-hover/folders:opacity-100"
                >
                  <Plus />
                </SidebarGroupAction>
                <CollapsibleContent>
                  <SidebarGroupContent>
                    <SidebarMenu>
                      {folders.map((folder) => (
                        <SidebarMenuItem key={folder.id}>
                          <SidebarMenuButton
                            isActive={scope === `folder:${folder.id}`}
                            tooltip={folder.name}
                            onClick={() => setScope(`folder:${folder.id}`)}
                          >
                            <Folder
                              fill={folderDotVar(folder.color)}
                              stroke="none"
                            />
                            <span className="truncate">{folder.name}</span>
                          </SidebarMenuButton>
                          <SidebarMenuBadge className="group-hover/menu-item:opacity-0">
                            {counts.byFolder.get(folder.id) ?? 0}
                          </SidebarMenuBadge>
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <SidebarMenuAction
                                showOnHover
                                aria-label={`${folder.name} options`}
                              >
                                <MoreHorizontal />
                              </SidebarMenuAction>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end" side="right">
                              <DropdownMenuItem
                                onClick={() => {
                                  setRenameFolder(folder);
                                  setRenameName(folder.name);
                                  setRenameColor(
                                    (FOLDER_COLORS.includes(
                                      folder.color as FolderColor,
                                    )
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
                        </SidebarMenuItem>
                      ))}
                    </SidebarMenu>
                  </SidebarGroupContent>
                </CollapsibleContent>
              </SidebarGroup>
            </Collapsible>
          </SidebarContent>
        </Sidebar>

        <section className="flex min-h-0 flex-1 flex-col">
          <div className="flex flex-wrap items-center gap-2 border-b border-border p-3">
            <SidebarTrigger />
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
      </SidebarProvider>

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
