import { create } from "zustand";

type PanelWidthsState = {
  sidebarWidth: number;
  splitPanelWidth: number;
  setSidebarWidth: (width: number) => void;
  setSplitPanelWidth: (width: number) => void;
};

/**
 * Shared only because the two panels' resize clamps depend on each other's
 * live width (Sidebar's max shrinks while SplitPanel is open wide, and vice
 * versa) — see docs/superpowers/specs/2026-08-20-app-shell-sidebar-
 * design.md § Resize. Zustand, not Context: same reasoning as
 * workbench/dock/dock-store.ts — two sibling components need read/write
 * access without every consumer re-rendering on every drag frame.
 */
export const usePanelWidthsStore = create<PanelWidthsState>((set) => ({
  sidebarWidth: 320,
  splitPanelWidth: 420,
  setSidebarWidth: (width) => set({ sidebarWidth: width }),
  setSplitPanelWidth: (width) => set({ splitPanelWidth: width }),
}));
