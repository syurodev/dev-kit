import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

type ModuleGroupsState = {
  /** The member of each group that was on screen last, by group id. */
  lastUsed: Record<string, string>;
  setLastUsed: (groupId: string, moduleId: string) => void;
};

/**
 * Which member a group's title-bar button opens. Kept across launches: the
 * button says "click me for what you were doing", and a relaunch that forgot
 * would send the click to Database after a day in Kafka.
 */
export const useModuleGroupsStore = create<ModuleGroupsState>()(
  persist(
    (set) => ({
      lastUsed: {},
      // A no-op when nothing changes: this runs on every module switch, and a
      // write to the UI-state store is not free.
      setLastUsed: (groupId, moduleId) =>
        set((state) =>
          state.lastUsed[groupId] === moduleId ? state : { lastUsed: { ...state.lastUsed, [groupId]: moduleId } },
        ),
    }),
    { name: "modules.groups", storage: uiStateStorage },
  ),
);
