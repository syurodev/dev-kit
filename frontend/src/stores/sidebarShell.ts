import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

type SidebarOpenState = {
  open: boolean;
  setOpen: (open: boolean) => void;
};

/**
 * Global, not per-module — closing the sidebar in one module closes it
 * everywhere until the user reopens it (docs/superpowers/specs/2026-08-20-
 * app-shell-sidebar-design.md). Default false (closed) matches the Figma
 * App Shell component's own default variant, Panel=Closed, Sidebar=Closed.
 */
export const useSidebarOpenStore = create<SidebarOpenState>()(
  persist(
    (set) => ({
      open: false,
      setOpen: (open) => set({ open }),
    }),
    { name: "sidebar", storage: uiStateStorage },
  ),
);
