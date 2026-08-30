import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type NoteSummary = {
  id: string;
  title: string;
  folderId: string;
  tags: string[];
  effectiveTags: string[];
  color: string;
  effectiveColor: string;
  preview: string;
  updatedAt: string;
  /** Empty for a live note. Set when the note is in the trash. */
  deletedAt: string;
};

export type Note = {
  id: string;
  title: string;
  body: string;
  folderId: string;
  tags: string[];
  effectiveTags: string[];
  color: string;
  effectiveColor: string;
};

export type NoteFolder = {
  id: string;
  name: string;
  color: string;
  tags: string[];
  updatedAt: string;
};

function asStrings(value: string[] | null | undefined): string[] {
  return value ?? [];
}

function mapSummary(n: {
  id: string;
  title: string;
  folder_id?: string;
  tags?: string[] | null;
  effective_tags?: string[] | null;
  color?: string;
  effective_color?: string;
  preview?: string;
  updated_at: string;
  deleted_at?: string;
}): NoteSummary {
  return {
    id: n.id,
    title: n.title,
    folderId: n.folder_id ?? "",
    tags: asStrings(n.tags),
    effectiveTags: asStrings(n.effective_tags),
    color: n.color ?? "",
    effectiveColor: n.effective_color ?? "",
    preview: n.preview ?? "",
    updatedAt: n.updated_at,
    deletedAt: n.deleted_at ?? "",
  };
}

function mapNote(n: {
  id: string;
  title: string;
  body: string;
  folder_id?: string;
  tags?: string[] | null;
  effective_tags?: string[] | null;
  color?: string;
  effective_color?: string;
}): Note {
  return {
    id: n.id,
    title: n.title,
    body: n.body,
    folderId: n.folder_id ?? "",
    tags: asStrings(n.tags),
    effectiveTags: asStrings(n.effective_tags),
    color: n.color ?? "",
    effectiveColor: n.effective_color ?? "",
  };
}

function mapFolder(f: {
  id: string;
  name: string;
  color: string;
  tags?: string[] | null;
  updated_at: string;
}): NoteFolder {
  return {
    id: f.id,
    name: f.name,
    color: f.color,
    tags: asStrings(f.tags),
    updatedAt: f.updated_at,
  };
}

export function listNotes(): Promise<NoteSummary[]> {
  return callBridge(async () => {
    const list = await AppService.NotesList();
    return (list ?? []).map(mapSummary);
  });
}

export function createNote(
  title: string,
  body: string,
  folderId: string,
  tags: string[],
): Promise<NoteSummary> {
  return callBridge(async () => {
    const n = await AppService.NotesCreate(title, body, folderId, tags);
    return mapSummary(n);
  });
}

export function updateNote(id: string, title: string, body: string): Promise<Note> {
  return callBridge(async () => {
    const n = await AppService.NotesUpdate(id, title, body);
    return mapNote(n);
  });
}

export function searchNotes(query: string): Promise<NoteSummary[]> {
  return callBridge(async () => {
    const list = await AppService.NotesSearch(query);
    return (list ?? []).map(mapSummary);
  });
}

export function getNote(id: string): Promise<Note> {
  return callBridge(async () => {
    const n = await AppService.NotesGet(id);
    return mapNote(n);
  });
}

export type LinkTarget = {
  id: string;
  title: string;
  deleted: boolean;
  missing: boolean;
};

/** The serialized form of a note-link node. Bodies are markdown, so the inline
 *  void has to survive a round trip through text; it carries the id and no
 *  label, because the label is the target's current title. */
export function noteLinkMarkup(noteId: string): string {
  return `[[note:${noteId}]]`;
}

export function listBacklinks(id: string): Promise<NoteSummary[]> {
  return callBridge(async () => {
    const list = await AppService.NotesListBacklinks(id);
    return (list ?? []).map(mapSummary);
  });
}

export function resolveNoteLinks(ids: string[]): Promise<LinkTarget[]> {
  return callBridge(async () => {
    const list = await AppService.NotesResolveLinks(ids);
    return (list ?? []).map((t) => ({
      id: t.id,
      title: t.title,
      deleted: !!t.deleted,
      missing: !!t.missing,
    }));
  });
}

export type NoteVersion = {
  id: string;
  noteId: string;
  trigger: "checkpoint" | "pre-restore";
  title: string;
  createdAt: string;
};

/** Version history is UI-only — no MCP tool reaches any of these. */
export function checkpointNote(id: string): Promise<NoteVersion | null> {
  return callBridge(async () => {
    const v = await AppService.NotesCheckpoint(id);
    // Checkpoint is a no-op when nothing changed, and says so with an empty id
    // rather than an error — an idle timer must not surface a failure.
    return v && v.id ? mapVersion(v) : null;
  });
}

export function listNoteVersions(id: string): Promise<NoteVersion[]> {
  return callBridge(async () => {
    const list = await AppService.NotesListVersions(id);
    return (list ?? []).map(mapVersion);
  });
}

export function getNoteVersion(versionId: string): Promise<{ title: string; body: string }> {
  return callBridge(async () => {
    const v = await AppService.NotesGetVersion(versionId);
    return { title: v?.title ?? "", body: v?.body ?? "" };
  });
}

export function restoreNoteVersion(id: string, versionId: string): Promise<Note> {
  return callBridge(async () => mapNote(await AppService.NotesRestoreVersion(id, versionId)));
}

function mapVersion(v: {
  id: string;
  note_id: string;
  trigger: string;
  title: string;
  created_at: string;
}): NoteVersion {
  return {
    id: v.id,
    noteId: v.note_id,
    trigger: v.trigger === "pre-restore" ? "pre-restore" : "checkpoint",
    title: v.title,
    createdAt: v.created_at,
  };
}

export type TrashedNote = NoteSummary & {
  deletedAt: string;
  purgesAt: string;
};

/** Trash is UI-only: none of these four have an MCP tool behind them. */
export function listTrashedNotes(): Promise<TrashedNote[]> {
  return callBridge(async () => {
    const list = await AppService.NotesListTrash();
    return (list ?? []).map((n) => ({
      ...mapSummary(n),
      deletedAt: n.deleted_at,
      purgesAt: n.purges_at,
    }));
  });
}

export function restoreNote(id: string): Promise<void> {
  return callBridge(() => AppService.NotesRestore(id) as Promise<void>);
}

export function purgeNote(id: string): Promise<void> {
  return callBridge(() => AppService.NotesPurge(id) as Promise<void>);
}

export function emptyTrash(): Promise<number> {
  return callBridge(() => AppService.NotesEmptyTrash() as Promise<number>);
}

export function deleteNote(id: string): Promise<void> {
  return callBridge(() => AppService.NotesDelete(id) as Promise<void>);
}

export function listFolders(): Promise<NoteFolder[]> {
  return callBridge(async () => {
    const list = await AppService.NotesFolderList();
    return (list ?? []).map(mapFolder);
  });
}

export function createFolder(
  name: string,
  color: string,
  tags: string[],
): Promise<NoteFolder> {
  return callBridge(async () => {
    const f = await AppService.NotesFolderCreate(name, color, tags);
    return mapFolder(f);
  });
}

export function updateFolder(
  id: string,
  name: string,
  color: string,
  tags: string[],
): Promise<NoteFolder> {
  return callBridge(async () => {
    const f = await AppService.NotesFolderUpdate(id, name, color, tags);
    return mapFolder(f);
  });
}

export function deleteFolder(id: string): Promise<void> {
  return callBridge(() => AppService.NotesFolderDelete(id) as Promise<void>);
}

export function setNoteOrganization(
  id: string,
  folderId: string,
  tags: string[],
  color: string,
): Promise<NoteSummary> {
  return callBridge(async () => {
    const n = await AppService.NotesSetOrganization(id, folderId, tags, color);
    return mapSummary(n);
  });
}
