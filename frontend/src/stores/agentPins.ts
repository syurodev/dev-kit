import { create } from "zustand";
import { persist } from "zustand/middleware";

import { uiStateStorage } from "@/bridge/uistate";

type AgentPinsState = {
  /** Session ids the user pinned, in the order they were pinned. */
  pinned: string[];
  isPinned: (sessionId: string) => boolean;
  togglePin: (sessionId: string) => void;
  unpin: (sessionId: string) => void;
};

/**
 * Which agents the user pinned to the top of the swarm tree.
 *
 * Deliberately not a column on agent_sessions: a pin is a per-person view
 * preference, not something about the agent, and putting it in the session row
 * would make it something an agent could read about itself and something the
 * bridge would have to authorize. ui_state is what this app already uses for
 * exactly this kind of state.
 *
 * Ids of deleted sessions are dropped as they are noticed rather than swept:
 * an id in this list that matches nothing simply never sorts anything.
 */
export const useAgentPinsStore = create<AgentPinsState>()(
  persist(
    (set, get) => ({
      pinned: [],
      isPinned: (sessionId) => get().pinned.includes(sessionId),
      togglePin: (sessionId) =>
        set((state) => ({
          pinned: state.pinned.includes(sessionId)
            ? state.pinned.filter((id) => id !== sessionId)
            : [...state.pinned, sessionId],
        })),
      unpin: (sessionId) =>
        set((state) => ({ pinned: state.pinned.filter((id) => id !== sessionId) })),
    }),
    {
      name: "agentPins",
      storage: uiStateStorage,
      partialize: (state) => ({ pinned: state.pinned }),
    },
  ),
);
