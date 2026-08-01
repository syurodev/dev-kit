import type { ModuleManifest } from "../registry";
import { NotesView } from "./NotesView";

export const notesModule: ModuleManifest = {
  id: "notes",
  title: "Notes",
  version: "1.0.0",
  apiVersion: "1",
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
  ],
};
