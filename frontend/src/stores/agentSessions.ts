import { create } from "zustand";
import { extractUpdateText, type AgentSession, type PendingPermission } from "../bridge/agenthub";

export type TranscriptLine = {
  id: string;
  role: "user" | "agent" | "system";
  text: string;
};

export type TranscriptState = {
  lines: TranscriptLine[];
  prompt: string;
};

export const EMPTY_TRANSCRIPT: TranscriptState = { lines: [], prompt: "" };

/** Append streaming chunk text for a session update payload. */
export function appendUpdateLine(
  lines: TranscriptLine[],
  update: unknown,
): TranscriptLine[] {
  const text = extractUpdateText(update).trim();
  if (!text) {
    return lines;
  }
  const last = lines[lines.length - 1];
  if (last?.role === "agent") {
    return [
      ...lines.slice(0, -1),
      { ...last, text: last.text === text ? last.text : `${last.text}${text}` },
    ];
  }
  return [
    ...lines,
    {
      id: `agent-${Date.now()}-${lines.length}`,
      role: "agent",
      text,
    },
  ];
}

type AgentSessionsState = {
  sessions: AgentSession[];
  activeSessionId: string;
  /**
   * The conversations themselves. These used to be useState inside
   * AgentHubView, which meant leaving the module unmounted the subtree and
   * threw the transcript away — and, worse, dropped the event subscription so
   * updates arriving while you were elsewhere were never collected at all.
   */
  transcripts: Record<string, TranscriptState>;
  pending: PendingPermission | null;
  /** Whether the shell is showing the conversation in the companion slot. */
  chatOpen: boolean;
  setSessions: (sessions: AgentSession[]) => void;
  upsertSession: (session: AgentSession) => void;
  removeSession: (id: string) => void;
  setActiveSessionId: (id: string) => void;
  setPrompt: (sessionId: string, prompt: string) => void;
  updateLines: (
    sessionId: string,
    updater: (prev: TranscriptLine[]) => TranscriptLine[],
  ) => void;
  setPending: (pending: PendingPermission | null) => void;
  setChatOpen: (open: boolean) => void;
};

export const useAgentSessionsStore = create<AgentSessionsState>((set) => ({
  sessions: [],
  activeSessionId: "",
  transcripts: {},
  pending: null,
  chatOpen: false,
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
  setPrompt: (sessionId, prompt) =>
    set((state) => ({
      transcripts: {
        ...state.transcripts,
        [sessionId]: { ...(state.transcripts[sessionId] ?? EMPTY_TRANSCRIPT), prompt },
      },
    })),
  updateLines: (sessionId, updater) =>
    set((state) => {
      const current = state.transcripts[sessionId] ?? EMPTY_TRANSCRIPT;
      return {
        transcripts: {
          ...state.transcripts,
          [sessionId]: { ...current, lines: updater(current.lines) },
        },
      };
    }),
  setPending: (pending) => set({ pending }),
  setChatOpen: (open) => set({ chatOpen: open }),
}));
