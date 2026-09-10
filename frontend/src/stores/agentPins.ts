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
 * Not a column on agent_sessions: a pin is a per-person view preference, not a
 * fact about the agent, and in the session row it becomes something an agent
 * could read about itself and the bridge would have to authorize. ui_state is
 * where this app already keeps such state.
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
