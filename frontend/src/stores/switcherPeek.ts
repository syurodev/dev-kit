import { create } from "zustand";

type SwitcherPeekState = {
  /** The switcher slot whose peek the keyboard asked for, or null. */
  slot: number | null;
  /** Opens a slot's peek; asking for the one that is open closes it. */
  toggle: (slot: number) => void;
  close: () => void;
};

/**
 * ⌘⌥1–⌘⌥9 opens a slot's peek. The key handler is the Workbench's and the
 * peeks are the switcher's buttons', with nothing between them but this.
 * Hovering is each button's own state and does not go through here.
 */
export const useSwitcherPeekStore = create<SwitcherPeekState>()((set) => ({
  slot: null,
  toggle: (slot) => set((state) => ({ slot: state.slot === slot ? null : slot })),
  close: () => set((state) => (state.slot === null ? state : { slot: null })),
}));
