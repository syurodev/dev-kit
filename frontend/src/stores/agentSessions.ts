import { create } from "zustand";
import type { AgentSession } from "../bridge/agenthub";

type AgentSessionsState = {
  sessions: AgentSession[];
  activeSessionId: string;
  setSessions: (sessions: AgentSession[]) => void;
  upsertSession: (session: AgentSession) => void;
  removeSession: (id: string) => void;
  setActiveSessionId: (id: string) => void;
};

export const useAgentSessionsStore = create<AgentSessionsState>((set) => ({
  sessions: [],
  activeSessionId: "",
  setSessions: (sessions) => set({ sessions }),
  upsertSession: (session) =>
    set((state) => {
      const index = state.sessions.findIndex((s) => s.id === session.id);
      if (index === -1) {
        return { sessions: [...state.sessions, session] };
      }
      const next = state.sessions.slice();
      next[index] = session;
      return { sessions: next };
    }),
  removeSession: (id) =>
    set((state) => ({
      sessions: state.sessions.filter((s) => s.id !== id),
    })),
  setActiveSessionId: (id) => set({ activeSessionId: id }),
}));
