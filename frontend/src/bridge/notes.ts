import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type NoteSummary = {
  id: string;
  title: string;
  updatedAt: string;
};

export type Note = {
  id: string;
  title: string;
  body: string;
};

export function listNotes(): Promise<NoteSummary[]> {
  return callBridge(async () => {
    const list = await AppService.NotesList();
    return (list ?? []).map((n) => ({
      id: n.id,
      title: n.title,
      updatedAt: n.updated_at,
    }));
  });
}

export function createNote(title: string, body: string): Promise<NoteSummary> {
  return callBridge(async () => {
    const n = await AppService.NotesCreate(title, body);
    return { id: n.id, title: n.title, updatedAt: n.updated_at };
  });
}

export function updateNote(id: string, title: string, body: string): Promise<Note> {
  return callBridge(async () => {
    const n = await AppService.NotesUpdate(id, title, body);
    return { id: n.id, title: n.title, body: n.body };
  });
}

export function searchNotes(query: string): Promise<NoteSummary[]> {
  return callBridge(async () => {
    const list = await AppService.NotesSearch(query);
    return (list ?? []).map((n) => ({
      id: n.id,
      title: n.title,
      updatedAt: n.updated_at,
    }));
  });
}

export function getNote(id: string): Promise<Note> {
  return callBridge(async () => {
    const n = await AppService.NotesGet(id);
    return { id: n.id, title: n.title, body: n.body };
  });
}

export function deleteNote(id: string): Promise<void> {
  return callBridge(() => AppService.NotesDelete(id) as Promise<void>);
}
