import { registerToolActivity, stringField } from "@/workbench/toolActivity";
import { parseNoteResult } from "./noteResult";

const preloadNoteCard = () => import("./NoteResultCard");

/**
 * What the notes.* tools that change something do (workbench/toolActivity.ts).
 * The title only, never the body: a note's text is decrypted content.
 */
export function registerNoteActivities(): void {
  for (const [name, running, done, failed] of [
    ["notes.create", "Creating note", "Created note", "Couldn't create note"],
    ["notes.update", "Updating note", "Updated note", "Couldn't update note"],
  ] as const) {
    registerToolActivity(name, {
      module: "notes",
      running,
      done,
      failed,
      preload: preloadNoteCard,
      target: ({ rawInput, rawOutput, state }) => {
        const note = state === "done" ? parseNoteResult(rawOutput) : null;
        if (note) return { label: note.title || "Untitled", ref: { module: "notes", kind: "note", id: note.id, label: note.title } };
        const title = stringField(rawInput, "title");
        return title ? { label: title } : null;
      },
    });
  }
  registerToolActivity("notes.delete", {
    module: "notes",
    running: "Deleting note",
    done: "Deleted note",
    failed: "Couldn't delete note",
    target: () => null,
  });
}
