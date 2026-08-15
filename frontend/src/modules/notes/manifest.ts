import { FileText } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

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
      ...lazyModuleView(() =>
        import("./NotesView").then(({ NotesView }) => ({ default: NotesView })),
      ),
    },
    {
      id: "notes.editor",
      title: "Note",
      ...lazyModuleView(() =>
        import("./NoteEditor").then(({ NoteEditor }) => ({ default: NoteEditor })),
      ),
    },
  ],
};
