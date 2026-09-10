import { create } from "zustand";
import type { ReactNode } from "react";

type SidebarContentState = {
  /** Registered sidebar content, keyed by the owning module's id. */
  content: Record<string, ReactNode>;
  setContent: (moduleId: string, node: ReactNode | null) => void;
};

/**
 * Zustand, not React Context — load-bearing here rather than a preference.
 *
 * Under Context the module calling useSidebarContent is itself a consumer, so
 * writing content re-renders it, producing a fresh JSX element, re-running the
 * registering effect: "Maximum update depth exceeded". React Compiler would
 * mask it by memoising the element, but it bails out of components that mutate
 * a ref during render (CodeGraphView does), so the loop was real.
 *
 * A store breaks the cycle: registering reads only the setter, whose identity is
 * stable, so a module never subscribes to its own registration. Only
 * SidebarSurface subscribes to `content`.
 *
 * Keyed by module id rather than one slot: modules stay mounted-but-hidden after
 * a visit (WorkbenchViews' mount policy), so a background module must not
 * clobber the active one's sidebar.
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
