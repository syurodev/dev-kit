import { FileText } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const notesModule: ModuleManifest = {
  id: "notes",
  title: "Notes",
  version: "1.0.0",
  apiVersion: "1",
  icon: FileText,
  // A tab per note (NotesView opens one per card, NoteEditor pins it on the
  // first edit) — the one module today with several items open at once.
  tabs: true,
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
