import { create } from "zustand";

type PanelWidthsState = {
  sidebarWidth: number;
  splitPanelWidth: number;
  /**
   * Height of the right column's TOP slot when both slots are open. Absolute,
   * not a ratio, so a drag means the same thing after the window changes —
   * splitColumn() clamps it at render instead.
   */
  splitTopHeight: number;
  /**
   * Whether each panel is currently occupying space. Both keep their last
   * dragged width above while closed, so the widths alone cannot tell the
   * cross-clamp how much room is actually free — see panelLayout.maxPanelWidth.
   */
  sidebarOpen: boolean;
  splitPanelOpen: boolean;
  setSplitTopHeight: (height: number) => void;
  setSidebarWidth: (width: number) => void;
  setSplitPanelWidth: (width: number) => void;
  setSidebarOpen: (open: boolean) => void;
  setSplitPanelOpen: (open: boolean) => void;
};

/**
 * Shared because the two panels' resize clamps depend on each other's live
 * width and open state (the sidebar's ceiling shrinks while the split panel is
 * open wide, and vice versa) — see docs/superpowers/specs/2026-08-20-app-
 * shell-sidebar-design.md § Resize. Zustand, not Context: same reasoning as
 * workbench/dock/dock-store.ts — two sibling components need read/write access
 * without every consumer re-rendering on every drag frame.
 */
export const usePanelWidthsStore = create<PanelWidthsState>((set) => ({
  sidebarWidth: 320,
  splitPanelWidth: 420,
  splitTopHeight: 430,
  sidebarOpen: false,
  splitPanelOpen: false,
  setSplitTopHeight: (height) => set({ splitTopHeight: height }),
  setSidebarWidth: (width) => set({ sidebarWidth: width }),
  setSplitPanelWidth: (width) => set({ splitPanelWidth: width }),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  setSplitPanelOpen: (open) => set({ splitPanelOpen: open }),
}));
