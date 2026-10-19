import { create } from "zustand";
import type { AgentImageAttachment } from "@/bridge/agenthub";

type ComposerImagesState = {
  byId: Record<string, AgentImageAttachment>;
  add: (image: AgentImageAttachment) => void;
  addAll: (images: readonly AgentImageAttachment[]) => void;
  forget: (ids: readonly string[]) => void;
};

/**
 * The bytes of the images chips in a draft stand for. A draft is a string and
 * an image chip in it holds only the id, so the picture itself waits here, in
 * memory, by id — not per session: a chip copied from one composer into another
 * (the rail's, another session's) must find its image there too. Nothing is
 * persisted: a draft restored after a relaunch has its words and its files, and
 * an image chip whose bytes are gone is dropped when the turn is built
 * (turnFromDraft) rather than sent empty.
 *
 * Entries are added when an image is attached or a queued prompt is taken back
 * for editing, and forgotten once the turn that used them was accepted.
 */
export const useComposerImagesStore = create<ComposerImagesState>()((set) => ({
  byId: {},
  add: (image) => set((state) => ({ byId: { ...state.byId, [image.id]: image } })),
  addAll: (images) =>
    set((state) => ({ byId: { ...state.byId, ...Object.fromEntries(images.map((image) => [image.id, image])) } })),
  forget: (ids) =>
    set((state) => {
      if (!ids.some((id) => id in state.byId)) return state;
      const byId = { ...state.byId };
      for (const id of ids) delete byId[id];
      return { byId };
    }),
}));
