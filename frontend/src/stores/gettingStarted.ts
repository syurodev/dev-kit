import { create } from "zustand";
import { persist } from "zustand/middleware";

import { uiStateStorage } from "@/bridge/uistate";

type GettingStartedState = {
  /** The person closed the first-run checklist. Kept across launches. */
  hidden: boolean;
  hide: () => void;
};

export const useGettingStartedStore = create<GettingStartedState>()(
  persist(
    (set) => ({
      hidden: false,
      hide: () => set({ hidden: true }),
    }),
    {
      name: "gettingStarted",
      storage: uiStateStorage,
      partialize: (state) => ({ hidden: state.hidden }),
    },
  ),
);
