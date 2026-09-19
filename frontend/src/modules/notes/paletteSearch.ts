import { FileText } from "lucide-react";
import type { NoteSummary } from "@/bridge/notes";
import type { PaletteSearchGroup } from "../registry";

const LIMIT = 8;
/** listNotes' own order is already "most recently updated first". */
const RECENT_LIMIT = 6;

export type NotesPaletteActions = {
  openNote: (note: NoteSummary) => void;
};

/** The pure half: notes already fetched, turned into one group for `query`. */
export function buildGroups(notes: NoteSummary[], actions: NotesPaletteActions): PaletteSearchGroup[] {
  if (notes.length === 0) return [];
  return [
    {
      heading: "NOTES",
      items: notes.map((note) => ({
        id: `note:${note.id}`,
        title: note.title || "Untitled",
        subtitle: note.preview,
        icon: FileText,
        run: () => actions.openNote(note),
      })),
    },
  ];
}

/**
 * Notes' contribution to the command palette.
 *
 * `searchNotes` is the backend's own full-text search — it can match inside a
 * note's body, which this side never sees more than a preview of — so a
 * result it returns is trusted and only capped to LIMIT, not re-scored and
 * potentially dropped by workbench/palette/match.ts against a title and
 * preview that may not be where the match actually was. An empty query
 * cannot go through it at all; the most recently updated few stand in, the
 * same "what would I probably want" a fresh dashboard leads with.
 */
export async function searchPalette(query: string, signal: AbortSignal): Promise<PaletteSearchGroup[]> {
  try {
    const [{ searchNotes, listNotes }, { openNoteTab }] = await Promise.all([
      import("@/bridge/notes"),
      import("./documents"),
    ]);

    const trimmed = query.trim();
    let notes: NoteSummary[];
    if (trimmed) {
      notes = (await searchNotes(trimmed)).slice(0, LIMIT);
    } else {
      notes = [...(await listNotes())]
        .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
        .slice(0, RECENT_LIMIT);
    }
    if (signal.aborted) return [];

    return buildGroups(notes, { openNote: (note) => openNoteTab(note, false) });
  } catch {
    return [];
  }
}
