import { create } from "zustand";
import type { ReactNode } from "react";

type SidebarContentState = {
  /** Registered sidebar content, keyed by the owning module's id. */
  content: Record<string, ReactNode>;
  setContent: (moduleId: string, node: ReactNode | null) => void;
};

/**
 * Zustand, not React Context — the same reasoning dock-store.ts spells out,
 * and here it is load-bearing rather than a preference.
 *
 * With a Context, the module calling useSidebarContent is itself a consumer,
 * so writing content changed the context value, which re-rendered that module,
 * which produced a fresh JSX element, which re-ran the registering effect —
 * an infinite loop ("Maximum update depth exceeded"). React Compiler would
 * have masked it by memoising the element, but it bails out of components that
 * mutate a ref during render (CodeGraphView does), so the loop was real.
 *
 * A store breaks the cycle: registering reads only the setter, whose identity
 * is stable for the store's lifetime, so a module never subscribes to its own
 * registration. Only SidebarSurface subscribes to `content`.
 *
 * Keyed by module id, not a single slot: modules stay mounted-but-hidden after
 * being visited (see WorkbenchViews' mount policy), so more than one can be
 * registered at a time and a background module must not clobber the active
 * one's sidebar.
 */
export const useSidebarContentStore = create<SidebarContentState>((set) => ({
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
