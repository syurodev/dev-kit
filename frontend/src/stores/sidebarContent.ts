import { create } from "zustand";
import type { ReactNode } from "react";

type SidebarContentState = {
  /** Registered sidebar content, keyed by the owning module's id. */
  content: Record<string, ReactNode>;
  /**
   * Who is registering, and what each one offered, per module. A module with
   * several documents open has several of them registering the same rail, so
   * the rail may only go away when the last one leaves — and when a temporary
   * registrar leaves (a Suspense skeleton), the rail has to fall back to what
   * a remaining one offered rather than keeping the skeleton's.
   */
  registrars: Record<string, Array<{ id: string; node: ReactNode }>>;
  setContent: (moduleId: string, node: ReactNode | null, registrar?: string) => void;
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
  registrars: {},
  setContent: (moduleId, node, registrar = moduleId) =>
    set((state) => {
      const current = state.registrars[moduleId] ?? [];

      if (node === null) {
        const remaining = current.filter((entry) => entry.id !== registrar);
        if (remaining.length === current.length) {
          return state;
        }
        if (remaining.length === 0) {
          const nextContent = { ...state.content };
          delete nextContent[moduleId];
          const nextRegistrars = { ...state.registrars };
          delete nextRegistrars[moduleId];
          return { content: nextContent, registrars: nextRegistrars };
        }
        // Someone is still here, so the rail stays — and it becomes THEIRS.
        // Falling through with the leaver's node is how a Suspense skeleton's
        // placeholder rail outlived the skeleton.
        return {
          content: { ...state.content, [moduleId]: remaining[remaining.length - 1].node },
          registrars: { ...state.registrars, [moduleId]: remaining },
        };
      }

      const without = current.filter((entry) => entry.id !== registrar);
      const registrars = [...without, { id: registrar, node }];
      if (
        state.content[moduleId] === node &&
        current.length === registrars.length &&
        current[current.length - 1]?.id === registrar
      ) {
        return state;
      }
      return {
        content: { ...state.content, [moduleId]: node },
        registrars: { ...state.registrars, [moduleId]: registrars },
      };
    }),
}));
