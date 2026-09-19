import { Bot } from "lucide-react";
import { rank } from "@/workbench/palette/match";
import type { AgentSession } from "@/bridge/agenthub";
import type { PaletteSearchGroup } from "../registry";

const LIMIT = 8;

/** `~/dev/acme-refunds` → `acme-refunds`. Mirrors codegraph's projectLabel,
 * kept local rather than imported across a module boundary for one line. */
function folderName(projectPath: string): string {
  const clean = projectPath.replace(/[\\/]+$/, "");
  const at = Math.max(clean.lastIndexOf("/"), clean.lastIndexOf("\\"));
  return at === -1 ? clean : clean.slice(at + 1);
}

/** Same label AgentHubSwitcher.tsx uses for a session with no title. */
export function sessionLabel(session: Pick<AgentSession, "title" | "agentType" | "id">): string {
  return session.title?.trim() || `${session.agentType} · ${session.id.slice(0, 8)}`;
}

export type AgentHubPaletteActions = {
  openSession: (session: AgentSession) => void;
};

/** The pure half: the sessions already in the store, turned into groups for `query`. */
export function buildGroups(
  query: string,
  sessions: AgentSession[],
  actions: AgentHubPaletteActions,
): PaletteSearchGroup[] {
  const matches = rank(
    query,
    sessions,
    (session) => ({
      title: sessionLabel(session),
      others: [session.agentType, session.projectPath, folderName(session.projectPath)],
    }),
    LIMIT,
  );
  if (matches.length === 0) return [];
  return [
    {
      heading: "SESSIONS",
      items: matches.map((session) => ({
        id: `session:${session.id}`,
        title: sessionLabel(session),
        subtitle: session.status ? `${folderName(session.projectPath)} · ${session.status}` : folderName(session.projectPath),
        icon: Bot,
        run: () => actions.openSession(session),
      })),
    },
  ];
}

/**
 * Agent Hub's contribution to the command palette: its sessions.
 *
 * The store is hot from launch (stores/agentSessions.ts subscribes as soon as
 * the app starts), so there is nothing to fetch here — read straight off it.
 */
export async function searchPalette(query: string, signal: AbortSignal): Promise<PaletteSearchGroup[]> {
  try {
    const [{ useAgentSessionsStore }, { showModuleHome }] = await Promise.all([
      import("@/stores/agentSessions"),
      import("@/workbench/showModule"),
    ]);
    if (signal.aborted) return [];

    const state = useAgentSessionsStore.getState();
    const sessions = state.sessionIds.map((id) => state.sessionsById[id]).filter(Boolean);

    return buildGroups(query, sessions, {
      openSession: (session) => {
        showModuleHome("agenthub");
        useAgentSessionsStore.getState().setActiveSessionId(session.id);
      },
    });
  } catch {
    return [];
  }
}
