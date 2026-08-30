import { useEffect, useRef } from "react";
import { create } from "zustand";
import type { LucideIcon } from "lucide-react";

export type ModuleAction = {
  id: string;
  icon: LucideIcon;
  label: string;
  isActive?: boolean;
  onClick: () => void;
};

type ModuleActionsState = {
  actions: ModuleAction[];
  registerAction: (action: ModuleAction) => void;
  unregisterAction: (id: string) => void;
};

export const useModuleActionsStore = create<ModuleActionsState>((set) => ({
  actions: [],
  registerAction: (action) =>
    set((state) => {
      const index = state.actions.findIndex((a) => a.id === action.id);
      if (index === -1) {
        return { actions: [...state.actions, action] };
      }
      const next = state.actions.slice();
      next[index] = action;
      return { actions: next };
    }),
  unregisterAction: (id) =>
    set((state) => ({ actions: state.actions.filter((a) => a.id !== id) })),
}));

/**
 * Registers `action` into the app-wide title-bar row for as long as the
 * calling component stays mounted (i.e. for as long as its module is
 * active). `onClick` is read through a ref so the registration effect only
 * re-runs when `id`/`icon`/`label`/`isActive` actually change — not on
 * every render of the calling component.
 */
export function useModuleAction(action: ModuleAction | null) {
  const registerAction = useModuleActionsStore((s) => s.registerAction);
  const unregisterAction = useModuleActionsStore((s) => s.unregisterAction);

  const onClickRef = useRef(action?.onClick);
  onClickRef.current = action?.onClick;

  const id = action?.id;
  const icon = action?.icon;
  const label = action?.label;
  const isActive = action?.isActive;

  // Two effects, not one. A single effect with isActive in its dependencies
  // unregistered the action on every toggle and registered it again, and
  // registerAction appends anything it has not seen — so toggling a module
  // action sent it to the end of the row, past its own siblings. Updating in
  // place keeps the position; removal belongs to unmount alone.
  useEffect(() => {
    if (!id || !icon || !label) {
      return;
    }
    registerAction({
      id,
      icon,
      label,
      isActive,
      onClick: () => onClickRef.current?.(),
    });
  }, [id, icon, label, isActive, registerAction]);

  useEffect(() => {
    if (!id) {
      return;
    }
    return () => unregisterAction(id);
  }, [id, unregisterAction]);
}
