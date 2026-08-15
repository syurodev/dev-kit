import { FileText } from "lucide-react";
import type { ModuleManifest } from "../registry";
import { NotesView } from "./NotesView";
import { NoteEditor } from "./NoteEditor";

export const notesModule: ModuleManifest = {
  id: "notes",
  title: "Notes",
  version: "1.0.0",
  apiVersion: "1",
  icon: FileText,
  capabilities: ["notes:read", "notes:write"],
  commands: [
    {
      id: "notes.create",
      title: "Create note",
    },
  ],
  views: [
    {
      id: "notes.list",
      title: "Notes",
      component: NotesView,
    },
    {
      id: "notes.editor",
      title: "Note",
      component: NoteEditor,
    },
  ],
};
