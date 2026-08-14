import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type NoteSummary = {
  id: string;
  title: string;
  folderId: string;
  tags: string[];
  effectiveTags: string[];
  preview: string;
  updatedAt: string;
};

export type Note = {
  id: string;
  title: string;
  body: string;
  folderId: string;
  tags: string[];
  effectiveTags: string[];
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
  preview?: string;
  updated_at: string;
}): NoteSummary {
  return {
    id: n.id,
    title: n.title,
    folderId: n.folder_id ?? "",
    tags: asStrings(n.tags),
    effectiveTags: asStrings(n.effective_tags),
    preview: n.preview ?? "",
    updatedAt: n.updated_at,
  };
}

function mapNote(n: {
  id: string;
  title: string;
  body: string;
  folder_id?: string;
  tags?: string[] | null;
  effective_tags?: string[] | null;
}): Note {
  return {
    id: n.id,
    title: n.title,
    body: n.body,
    folderId: n.folder_id ?? "",
    tags: asStrings(n.tags),
    effectiveTags: asStrings(n.effective_tags),
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
): Promise<NoteSummary> {
  return callBridge(async () => {
    const n = await AppService.NotesSetOrganization(id, folderId, tags);
    return mapSummary(n);
  });
}
