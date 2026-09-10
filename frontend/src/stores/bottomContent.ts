import { create } from "zustand";
import type { ReactNode } from "react";

type BottomContentState = {
  /** Registered bottom-slot content, keyed by the owning module's id. */
  content: Record<string, ReactNode>;
  setContent: (moduleId: string, node: ReactNode | null) => void;
};

/**
 * The bottom slot's content registry — the same shape, and the same three
 * reasons, as stores/sidebarContent.ts. Read that file's doc comment before
 * changing this one; all of it applies here.
 *
 * In short: Zustand rather than Context (a registering module is itself a
 * consumer, so registration feeds back into its own render), keyed by module id
 * (several are mounted at once), identity-checked on write (the registering
 * effect depends on the node it registers).
 */
export const useBottomContentStore = create<BottomContentState>((set) => ({
  content: {},
  setContent: (moduleId, node) =>
    set((state) => {
      if (node === null) {
        if (!(moduleId in state.content)) {
          return state;
        }
        const next = { ...state.content };
        delete next[moduleId];
        return { content: next };
      }
      if (state.content[moduleId] === node) {
        return state;
      }
      return { content: { ...state.content, [moduleId]: node } };
    }),
}));
