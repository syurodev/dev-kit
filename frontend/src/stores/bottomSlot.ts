import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

type BottomSlotState = {
  open: boolean;
  /** Live drag height. 220 matches the Figma App Shell's Bottom=Open variant. */
  height: number;
  setOpen: (open: boolean) => void;
  setHeight: (height: number) => void;
};

/**
 * Global, not per-module — same as stores/sidebarShell.ts: closing the slot in
 * one module closes it everywhere until the user reopens it. Default false so
 * the shell still opens on the Figma component's default variant.
 *
 * Only `open` is persisted; the height is session-only, matching
 * stores/panelWidths.ts. Persisting one panel's size and not the others' is a
 * worse inconsistency than persisting none.
 */
export const useBottomSlotStore = create<BottomSlotState>()(
  persist(
    (set) => ({
      open: false,
      height: 220,
      setOpen: (open) => set({ open }),
      setHeight: (height) => set({ height }),
    }),
    {
      name: "bottomSlot",
      storage: uiStateStorage,
      partialize: (state) => ({ open: state.open }),
    },
  ),
);
