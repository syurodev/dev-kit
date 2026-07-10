import type { ModuleDefinition } from "../registry";
import { NotesView } from "./NotesView";

export const notesModule: ModuleDefinition = {
  id: "notes",
  title: "Notes",
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
  ],
};
