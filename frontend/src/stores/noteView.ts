import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

export type NoteFont = "default" | "serif" | "mono";

type NoteViewState = {
  /** Note ids reading edge-to-edge instead of the centred writing column. */
  fullWidth: string[];
  /** Note ids set in 14px body text instead of 16px. */
  smallText: string[];
  /** Note ids that open read-only until unlocked here. */
  locked: string[];
  /** A note's typeface when it is not the default one. */
  fonts: Record<string, Exclude<NoteFont, "default">>;
  toggleFullWidth: (noteId: string) => void;
  toggleSmallText: (noteId: string) => void;
  toggleLocked: (noteId: string) => void;
  setFont: (noteId: string, font: NoteFont) => void;
};

function toggled(ids: string[], id: string): string[] {
  return ids.includes(id) ? ids.filter((other) => other !== id) : [...ids, id];
}

/**
 * How each note is shown — Notion's per-page "Appearance & layout": typeface,
 * small text, full width, and a lock against accidental edits. A per-person
 * view preference, not a fact about the note, so it lives in ui_state rather
 * than vault-item metadata: it never syncs, and it never becomes something an
 * MCP tool could read or set (so "locked" stops this window's editor, not an
 * agent or another device). An id whose note was deleted just never matches
 * anything again — not swept, the same trade-off agentPins.ts makes for the
 * same reason. Consumers select a field and check `.includes(id)` themselves
 * (agentPins.ts's own convention) rather than a selector that calls back into
 * the store.
 *
 * The storage key is the one full width alone used to have: renaming it would
 * forget every note already set full width. Fields added since come back as
 * their defaults from an older saved value (persist merges shallowly).
 */
export const useNoteViewStore = create<NoteViewState>()(
  persist(
    (set) => ({
      fullWidth: [],
      smallText: [],
      locked: [],
      fonts: {},
      toggleFullWidth: (noteId) => set((state) => ({ fullWidth: toggled(state.fullWidth, noteId) })),
      toggleSmallText: (noteId) => set((state) => ({ smallText: toggled(state.smallText, noteId) })),
      toggleLocked: (noteId) => set((state) => ({ locked: toggled(state.locked, noteId) })),
      setFont: (noteId, font) =>
        set((state) => {
          const fonts = { ...state.fonts };
          if (font === "default") delete fonts[noteId];
          else fonts[noteId] = font;
          return { fonts };
        }),
    }),
    {
      name: "notes.fullWidth",
      storage: uiStateStorage,
      partialize: (s) => ({ fullWidth: s.fullWidth, smallText: s.smallText, locked: s.locked, fonts: s.fonts }),
    },
  ),
);
