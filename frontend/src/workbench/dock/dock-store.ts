import { create } from "zustand";
import type { DockEntry } from "./types";

/**
 * Items a module contributes to the dock while it's active (e.g. a future
 * "+ New profile" quick action for Database) — separate from the 8 fixed
 * module-switcher icons, which live directly in Dock.tsx and never change.
 * Zustand, not React Context: contextItems/hover change often, and a plain
 * Context re-renders every consumer on every value change unless each one
 * is manually wrapped in useMemo — Zustand's selector subscriptions avoid
 * that without needing to remember the workaround at every call site.
 */
type DockContextState = {
  contextItems: DockEntry[];
  setContextItems: (items: DockEntry[]) => void;
  clearContextItems: () => void;
};

export const useDockStore = create<DockContextState>((set) => ({
  contextItems: [],
  setContextItems: (items) => set({ contextItems: items }),
  clearContextItems: () => set({ contextItems: [] }),
}));
