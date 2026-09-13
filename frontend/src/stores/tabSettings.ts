import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

/**
 * How wide a workbench tab may get (Settings ▸ Appearance ▸ Tabs) — VS Code's
 * tabSizingFixedMinWidth / tabSizingFixedMaxWidth. The strip shares the room
 * equally between the two and scrolls past the minimum.
 */
export const TAB_WIDTH_LIMITS = { min: 60, max: 480 } as const;

/**
 * Both numbers inside the limits, and the max never under the min — a max
 * below the min would let CSS quietly pick one and the setting would lie.
 */
export function clampTabWidths(minWidth: number, maxWidth: number) {
  const min = Math.min(Math.max(Math.round(minWidth), TAB_WIDTH_LIMITS.min), TAB_WIDTH_LIMITS.max);
  const max = Math.min(Math.max(Math.round(maxWidth), min), TAB_WIDTH_LIMITS.max);
  return { minWidth: min, maxWidth: max };
}

type TabSettingsState = {
  minWidth: number;
  maxWidth: number;
  setTabWidths: (patch: Partial<{ minWidth: number; maxWidth: number }>) => void;
};

export const useTabSettingsStore = create<TabSettingsState>()(
  persist(
    (set, get) => ({
      minWidth: 80,
      maxWidth: 320,
      setTabWidths: (patch) =>
        set(clampTabWidths(patch.minWidth ?? get().minWidth, patch.maxWidth ?? get().maxWidth)),
    }),
    { name: "tabs.sizing", storage: uiStateStorage },
  ),
);
