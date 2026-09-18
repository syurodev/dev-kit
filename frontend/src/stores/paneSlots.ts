import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";
import { DEFAULT_PLACEMENT, move, type PaneId, type Placement, type SlotId } from "@/workbench/slots/slotModel";

type PaneSlotsState = {
  /** Companions: one place each, whichever module is active. */
  agent: SlotId;
  terminal: SlotId;
  /** The context pane's place, per module that has ever moved it. */
  contextByModule: Record<string, SlotId>;
  /** Column B's width; column A keeps usePanelWidthsStore.splitPanelWidth. */
  columnBWidth: number;
  /** Column B's top slot height when split; A's is splitTopHeight. */
  columnBTopHeight: number;
  movePane: (pane: PaneId, to: SlotId, moduleId: string | null) => void;
  setColumnBWidth: (width: number) => void;
  setColumnBTopHeight: (height: number) => void;
};

export function placementFor(s: Pick<PaneSlotsState, "agent" | "terminal" | "contextByModule">, moduleId: string | null): Placement {
  return {
    context: (moduleId && s.contextByModule[moduleId]) || DEFAULT_PLACEMENT.context,
    agent: s.agent,
    terminal: s.terminal,
  };
}

/**
 * Where each pane sits in the slot layout (workbench/slots/slotModel.ts).
 *
 * Persisted like the bottom slot's open state: a layout you arranged is still
 * there after a restart. Sizes are not, matching stores/panelWidths.ts.
 */
export const usePaneSlotsStore = create<PaneSlotsState>()(
  persist(
    (set) => ({
      agent: DEFAULT_PLACEMENT.agent,
      terminal: DEFAULT_PLACEMENT.terminal,
      contextByModule: {},
      columnBWidth: 360,
      columnBTopHeight: 430,
      movePane: (pane, to, moduleId) =>
        set((s) => {
          const next = move(placementFor(s, moduleId), pane, to);
          return {
            agent: next.agent,
            terminal: next.terminal,
            // Only the active module's context pane is on screen, so only its
            // place can have been dragged.
            contextByModule: moduleId ? { ...s.contextByModule, [moduleId]: next.context } : s.contextByModule,
          };
        }),
      setColumnBWidth: (width) => set({ columnBWidth: width }),
      setColumnBTopHeight: (height) => set({ columnBTopHeight: height }),
    }),
    {
      name: "paneSlots",
      storage: uiStateStorage,
      partialize: (s) => ({ agent: s.agent, terminal: s.terminal, contextByModule: s.contextByModule }),
    },
  ),
);
