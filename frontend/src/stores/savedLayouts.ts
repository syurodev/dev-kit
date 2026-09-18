import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";
import type { LayoutState } from "@/workbench/slots/layoutPresets";

export type SavedLayout = { id: string; name: string; state: LayoutState };

type SavedLayoutsState = {
  /** Layouts saved from the Layouts menu, per module. */
  byModule: Record<string, SavedLayout[]>;
  save: (moduleId: string, state: LayoutState) => void;
  forget: (moduleId: string) => void;
};

/**
 * "Save current layout" in the Layouts menu (workbench/LayoutMenu.tsx): the
 * pane arrangement for one module, to come back to. Persisted — a layout you
 * saved is one you meant to keep.
 */
export const useSavedLayoutsStore = create<SavedLayoutsState>()(
  persist(
    (set) => ({
      byModule: {},
      save: (moduleId, state) =>
        set((s) => {
          const list = s.byModule[moduleId] ?? [];
          const entry = { id: `${Date.now().toString(36)}`, name: `Saved layout ${list.length + 1}`, state };
          return { byModule: { ...s.byModule, [moduleId]: [...list, entry] } };
        }),
      forget: (moduleId) =>
        set((s) => {
          const { [moduleId]: _, ...rest } = s.byModule;
          return { byModule: rest };
        }),
    }),
    { name: "savedLayouts", storage: uiStateStorage, partialize: (s) => ({ byModule: s.byModule }) },
  ),
);
