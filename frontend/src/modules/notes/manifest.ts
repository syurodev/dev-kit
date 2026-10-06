import { lazy, createElement } from "react";
import { FileText } from "lucide-react";
import { PLAN_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { registerIntent } from "@/workbench/intents";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { registerNoteActivities } from "./toolActivities";
import { parseNoteResult } from "./noteResult";

export const notesModule: ModuleManifest = {
  id: "notes",
  title: "Notes",
  // The dashboard is a list, not a new document — opening it creates nothing. Its home costs nothing to open, so it opens
  // rather than showing "has nothing open" (ModuleManifest.opensHome).
  opensHome: true,
  version: "1.0.0",
  apiVersion: "1",
  icon: FileText,
  group: PLAN_GROUP,
  // A tab per note (NotesView opens one per card, NoteEditor pins it on the
  // first edit) — the one module today with several items open at once.
  tabs: true,
  // The rail is the module's; its views register over it. Without this a Notes
  // window with every note closed has no rail at all.
  sidebar: lazy(() =>
    import("./useNotesSidebar").then(({ NotesModuleSidebar }) => ({
      default: NotesModuleSidebar,
    })),
  ),
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
  paletteSearch: {
    noun: "notes",
    search: (query, signal) => import("./paletteSearch").then((m) => m.searchPalette(query, signal)),
  },
};

/**
 * `notes.reveal` (docs/dev-kit/cross-module.mdx): open a note from anywhere —
 * a ToolResultView card for `notes.create`/`notes.update` — without the
 * caller importing this module's documents helper. Registered with the
 * manifest, which is eager; `./documents` loads on the first dispatch.
 */
registerIntent(
  "notes.reveal",
  async (ref, { placement }) => {
    if (ref.kind !== "note") return;
    const { openNoteTab } = await import("./documents");
    openNoteTab({ id: ref.id, title: ref.label }, false, placement === "beside");
  },
  "beside",
);

/**
 * ToolResultView shared by notes.create and notes.update
 * (docs/dev-kit/tool-result-view.mdx) — one card component, one
 * registration per tool since the registry keys on the tool, not the shape.
 */
const LazyNoteResultCard = lazy(() =>
  import("./NoteResultCard").then(({ NoteResultCard }) => ({ default: NoteResultCard })),
);

function registerNoteResultView(toolName: string): void {
  registerToolResultView(toolName, ({ rawOutput, heading }) => {
    const note = parseNoteResult(rawOutput);
    if (!note) return null;
    return createElement(LazyNoteResultCard, { note, heading });
  });
}

for (const toolName of ["notes.create", "notes.update"]) {
  registerNoteResultView(toolName);
}

// What this module's tools that change something do, for the transcript's
// always-visible cards (workbench/toolActivity.ts).
registerNoteActivities();
