import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";
import { DEFAULT_SLOT, contextPane, moduleOfPane, type Placement, type SlotId } from "@/workbench/slots/slotModel";

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
  /** Writes back a placement: companions globally, context panes per module. */
  applyPlacement: (placement: Placement) => void;
  setColumnBWidth: (width: number) => void;
  setColumnBTopHeight: (height: number) => void;
};

/** The placement of the companions and of the context panes of `modules`. */
export function placementFor(
  s: Pick<PaneSlotsState, "agent" | "terminal" | "contextByModule">,
  modules: readonly string[],
): Placement {
  const out: Placement = { agent: s.agent, terminal: s.terminal };
  for (const m of modules) out[contextPane(m)] = s.contextByModule[m] ?? DEFAULT_SLOT.context ?? "a.top";
  return out;
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
      agent: DEFAULT_SLOT.agent,
      terminal: DEFAULT_SLOT.terminal,
      contextByModule: {},
      columnBWidth: 360,
      columnBTopHeight: 430,
      applyPlacement: (placement) =>
        set((s) => {
          const contextByModule = { ...s.contextByModule };
          for (const [pane, slot] of Object.entries(placement)) {
            const m = moduleOfPane(pane);
            if (m) contextByModule[m] = slot;
          }
          return { agent: placement.agent ?? s.agent, terminal: placement.terminal ?? s.terminal, contextByModule };
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
