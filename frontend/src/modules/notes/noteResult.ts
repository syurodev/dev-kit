import { parseToolOutput } from "@/workbench/toolResultViews";

/**
 * The one field the card shows. notes.create answers with
 * internal/notes/notes.go: Summary (id, title, preview, …) and notes.update
 * with the full Note (id, title, body, …) — different shapes, but both carry
 * id and title, and the card shows nothing else: preview/body is the note's
 * decrypted content, and a transcript card is not the place to surface it
 * (it's already less exposure than today's JSON fallback, which does show
 * preview/body — but no reason to keep that going forward).
 */
export type NoteResult = {
  id: string;
  title: string;
};

function isNoteResult(value: unknown): value is NoteResult {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return typeof v.id === "string" && typeof v.title === "string";
}

/** Guards a notes.* tool's result before the Card component is imported. */
export function parseNoteResult(rawOutput: unknown): NoteResult | null {
  const parsed = parseToolOutput(rawOutput);
  return isNoteResult(parsed) ? parsed : null;
}
