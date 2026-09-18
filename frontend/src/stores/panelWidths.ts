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
  /** What each right column occupies now, sash included; 0 while it is not drawn. */
  columnWidths: { a: number; b: number };
  setSplitTopHeight: (height: number) => void;
  setSidebarWidth: (width: number) => void;
  setSplitPanelWidth: (width: number) => void;
  setSidebarOpen: (open: boolean) => void;
  setColumnWidth: (column: "a" | "b", width: number) => void;
};

/**
 * Shared because the two panels' resize clamps depend on each other's live
 * width and open state (the sidebar's ceiling shrinks while the split panel is
 * open wide, and vice versa) — see docs/superpowers/specs/2026-08-20-app-
 * shell-sidebar-design.md § Resize. Zustand, not Context: two sibling components need read/write access
 * without every consumer re-rendering on every drag frame.
 */
export const usePanelWidthsStore = create<PanelWidthsState>((set) => ({
  sidebarWidth: 280,
  splitPanelWidth: 320,
  splitTopHeight: 430,
  sidebarOpen: false,
  columnWidths: { a: 0, b: 0 },
  setSplitTopHeight: (height) => set({ splitTopHeight: height }),
  setSidebarWidth: (width) => set({ sidebarWidth: width }),
  setSplitPanelWidth: (width) => set({ splitPanelWidth: width }),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  setColumnWidth: (column, width) =>
    set((s) => (s.columnWidths[column] === width ? s : { columnWidths: { ...s.columnWidths, [column]: width } })),
}));
