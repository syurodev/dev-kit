import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

type NoteWidthState = {
  /** Note ids reading edge-to-edge instead of the centred writing column. */
  fullWidth: string[];
  toggleFullWidth: (noteId: string) => void;
};

/**
 * Which notes read full width instead of the centred ~720px column —
 * Notion's per-page "Full width" toggle. A per-person view preference, not a
 * fact about the note, so it lives in ui_state rather than vault-item
 * metadata: it never syncs, and it never becomes something an MCP tool could
 * read or set. An id whose note was deleted just never matches anything
 * again — not swept, the same trade-off agentPins.ts makes for the same
 * reason. Consumers select `fullWidth` and check `.includes(id)` themselves
 * (agentPins.ts's own convention) rather than a selector that calls back
 * into the store.
 */
export const useNoteWidthStore = create<NoteWidthState>()(
  persist(
    (set) => ({
      fullWidth: [],
      toggleFullWidth: (noteId) =>
        set((state) => ({
          fullWidth: state.fullWidth.includes(noteId)
            ? state.fullWidth.filter((id) => id !== noteId)
            : [...state.fullWidth, noteId],
        })),
    }),
    { name: "notes.fullWidth", storage: uiStateStorage },
  ),
);
