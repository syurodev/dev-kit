import type { AgentSession } from "@/bridge/agenthub";
import type { PendingElicitation, PendingPermission } from "@/bridge/agenthub";
import type { Subagent } from "../types";
import { projectLabel, sessionGroup, type SidebarWorkspace } from "./grouping";

/**
 * The pure half of the Agent Hub rail: which group a session is filed in, in
 * what order, which cards show and which fold into a stack, what needs the
 * person. Nothing here touches React or a store, so every rule is a test.
 */

/** Ordinary recent sessions a group shows before the rest fold into its stack. */
export const RECENT_LIMIT = 5;

/** A rail narrower than this drops the time and one avatar (see useRailWidth). */
export const NARROW_RAIL_WIDTH = 240;

// ---------------------------------------------------------------------------
// Attention

/** What one session is waiting on the person for, from the two queues. */
export type Attention = { permissions: number; questions: number };

/**
 * Permissions and questions by session. Both stop the agent until someone
 * answers, but they are different asks: a permission also changes the
 * session's status, a question does not (the turn is still running), which is
 * why `session.status` alone cannot be the whole answer.
 */
export function attentionBySession(
  permissions: readonly Pick<PendingPermission, "sessionId">[],
  questions: readonly Pick<PendingElicitation, "sessionId">[],
): Record<string, Attention> {
  const out: Record<string, Attention> = {};
  for (const p of permissions) {
    const at = out[p.sessionId] ?? { permissions: 0, questions: 0 };
    out[p.sessionId] = { ...at, permissions: at.permissions + 1 };
  }
  for (const q of questions) {
    const at = out[q.sessionId] ?? { permissions: 0, questions: 0 };
    out[q.sessionId] = { ...at, questions: at.questions + 1 };
  }
  return out;
}

export type AttentionKind = "permission" | "question";

/**
 * What the session is waiting on, if anything. A permission wins when both
 * are open: it has a deadline behind it, a question does not.
 */
export function attentionKind(session: Pick<AgentSession, "status">, attention?: Attention): AttentionKind | null {
  if (session.status === "awaiting_permission" || (attention?.permissions ?? 0) > 0) return "permission";
  if ((attention?.questions ?? 0) > 0) return "question";
  return null;
}

export type CardIndicator = "permission" | "question" | "error" | "running" | null;

/**
 * The one state glyph a card shows. Idle and closed show none: a dot on every
 * card is decoration, and the glyphs that remain have to stand out.
 */
export function cardIndicator(session: Pick<AgentSession, "status">, attention?: Attention): CardIndicator {
  const waiting = attentionKind(session, attention);
  if (waiting) return waiting;
  if (session.status === "error") return "error";
  if (session.status === "running") return "running";
  return null;
}

// ---------------------------------------------------------------------------
// Grouping

export type SidebarGroup = {
  /** The collapse key, shared with the old tree so a folded group stays folded. */
  key: string;
  kind: "workspace" | "folder" | "scratch";
  /** The folder a "+" starts in: a workspace's primary, else the path itself. */
  path: string;
  label: string;
  workspace?: SidebarWorkspace;
  /** Every session filed here, children included, in the order they came. */
  sessions: AgentSession[];
};

/**
 * Sessions by workspace / folder.
 *
 * Membership is the existing rule (sessionGroup), applied to a session's
 * lineage root: a child stays in its parent's group even when it was opened on
 * another path, because lineage is the boundary an agent may act within. A
 * child whose parent is missing from the list is a root of its own project
 * rather than dropped.
 *
 * Grouping is by the group's key, never by its label: two folders called
 * `api` are two groups, told apart by their parent folder.
 */
export function groupSessions(
  sessions: readonly AgentSession[],
  scratchRoot = "",
  workspaces: SidebarWorkspace[] = [],
): SidebarGroup[] {
  const byId = new Map(sessions.map((s) => [s.id, s]));
  const rootOf = (session: AgentSession): AgentSession => {
    const seen = new Set<string>([session.id]);
    let at = session;
    for (;;) {
      const parent = at.parentSessionId ? byId.get(at.parentSessionId) : undefined;
      if (!parent || seen.has(parent.id)) return at;
      seen.add(parent.id);
      at = parent;
    }
  };

  const groups = new Map<string, { workspace?: SidebarWorkspace; sessions: AgentSession[] }>();
  for (const session of sessions) {
    const { key, workspace } = sessionGroup(rootOf(session), scratchRoot, workspaces);
    const group = groups.get(key) ?? { workspace, sessions: [] };
    group.sessions.push(session);
    groups.set(key, group);
  }

  // Two folders with the same name (an old clone and a new one) would be two
  // rows that read the same; those say which parent they are in.
  const folderNames = new Map<string, number>();
  for (const [key, group] of groups) {
    if (group.workspace || key === scratchRoot) continue;
    const name = projectLabel(key);
    folderNames.set(name, (folderNames.get(name) ?? 0) + 1);
  }

  const out: SidebarGroup[] = [];
  for (const [key, group] of groups) {
    const scratch = !group.workspace && Boolean(scratchRoot) && key === scratchRoot;
    const name = projectLabel(key);
    const folderLabel = (folderNames.get(name) ?? 0) > 1 ? projectLabel(key, 2) : name;
    out.push({
      // The tree's own keys, so a group folded there is folded here.
      key: group.workspace ? key : `project:${key}`,
      kind: group.workspace ? "workspace" : scratch ? "scratch" : "folder",
      path: group.workspace ? (group.workspace.paths[0] ?? "") : key,
      label: group.workspace ? group.workspace.name : scratch ? "No project" : folderLabel || "No project",
      ...(group.workspace ? { workspace: group.workspace } : {}),
      sessions: group.sessions,
    });
  }
  // No project last: the named ones are what the rail is organised by.
  return out.sort(
    (a, b) => Number(a.kind === "scratch") - Number(b.kind === "scratch") || a.label.localeCompare(b.label),
  );
}

/**
 * Closed sessions out of the rail. The one on screen stays: a row that
 * vanishes from under the person who is reading it is a surprise.
 */
export function withoutClosed(sessions: readonly AgentSession[], hideClosed: boolean, activeId?: string): AgentSession[] {
  return hideClosed ? sessions.filter((s) => s.status !== "closed" || s.id === activeId) : [...sessions];
}

// ---------------------------------------------------------------------------
// Order and the 5 + stack split

/**
 * Most recent activity first. `updatedAt` is the backend's row time — status
 * and title changes, not transcript chunks — so a streaming agent does not
 * reorder the list token by token. Equal (or both unknown) times fall back to
 * the id, which never changes, so ties cannot swap places between renders.
 */
export function compareRecent(a: AgentSession, b: AgentSession): number {
  const delta = (b.updatedAt ?? 0) - (a.updatedAt ?? 0);
  if (delta !== 0) return delta;
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

export type PartitionOptions = {
  activeId?: string;
  /** Pinned ids, in pin order. */
  pinnedIds?: readonly string[];
  /** Sessions waiting on the person. */
  waiting?: (session: AgentSession) => boolean;
  limit?: number;
};

export type GroupView = {
  /** Cards drawn while the group is open, in display order. */
  shown: AgentSession[];
  /** Sessions folded into the stack, newest first. Exactly the ones not drawn. */
  stacked: AgentSession[];
};

/**
 * Which cards a group draws and which fold into its stack.
 *
 * A session is drawn when it is among the `limit` most recent, or an exception:
 * the one on screen, one waiting on the person, a pinned one. Exceptions are
 * added to the recent set, never counted against it, so a session turning into
 * an exception only ever appears — nothing else moves into the stack because
 * of it — and one in both sets is drawn once.
 *
 * Display order is pinned first (in pin order), then by recency. A session
 * pulled out of the stack therefore lands where its age puts it, below the
 * recent ones and above the stack row, and stays there as long as it is an
 * exception. Running is not an exception: a group full of running agents
 * would otherwise never fold.
 */
export function partitionGroup(sessions: readonly AgentSession[], options: PartitionOptions = {}): GroupView {
  const { activeId, pinnedIds = [], waiting, limit = RECENT_LIMIT } = options;
  const ordered = [...sessions].sort(compareRecent);
  const pinnedAt = new Map(pinnedIds.map((id, at) => [id, at]));

  const keep = new Set<string>();
  for (const s of ordered.slice(0, Math.max(0, limit))) keep.add(s.id);
  for (const s of ordered) {
    if (s.id === activeId || pinnedAt.has(s.id) || waiting?.(s)) keep.add(s.id);
  }

  const kept = ordered.filter((s) => keep.has(s.id));
  const pinned = kept
    .filter((s) => pinnedAt.has(s.id))
    .sort((a, b) => (pinnedAt.get(a.id) ?? 0) - (pinnedAt.get(b.id) ?? 0));
  const rest = kept.filter((s) => !pinnedAt.has(s.id));
  return { shown: [...pinned, ...rest], stacked: ordered.filter((s) => !keep.has(s.id)) };
}

/**
 * The next session waiting on the user, after the one selected — so a "N
 * waiting on you" footer is a place to go, not just a count, and pressing it
 * again walks on to the next one.
 */
export function nextWaiting(waitingIds: string[], activeSessionId?: string): string | undefined {
  if (waitingIds.length === 0) return undefined;
  const at = activeSessionId ? waitingIds.indexOf(activeSessionId) : -1;
  return waitingIds[(at + 1) % waitingIds.length];
}

/** How many sessions in the group are waiting on the person. */
export function waitingCount(
  sessions: readonly AgentSession[],
  attention: Record<string, Attention>,
): number {
  let n = 0;
  for (const s of sessions) if (attentionKind(s, attention[s.id])) n++;
  return n;
}

// ---------------------------------------------------------------------------
// Labels

/**
 * "now", "10m", "2h", "3d", "2w", "5mo", "1y": the shortest honest age. Empty
 * when the time is unknown, so a card shows nothing rather than "0m".
 */
export function shortAge(at: number | undefined, now: number): string {
  if (!at) return "";
  const seconds = Math.max(0, Math.floor((now - at) / 1000));
  if (seconds < 60) return "now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days}d`;
  if (days < 30) return `${Math.floor(days / 7)}w`;
  if (days < 365) return `${Math.floor(days / 30)}mo`;
  return `${Math.floor(days / 365)}y`;
}

export type SubagentState = "running" | "done" | "failed";

/**
 * A subagent reports end states and nothing at all while it works, since the
 * spawn notification carries no state. No state means it is still going.
 * (Killed ones never reach the rail: stores/agentSessions drops them.)
 */
export function subagentState(state: string | undefined): SubagentState {
  if (state === "completed") return "done";
  if (state === "failed") return "failed";
  return "running";
}

const SUBAGENT_STATE_WORD: Record<SubagentState, string> = { running: "running", done: "done", failed: "failed" };

export function subagentStateWord(state: string | undefined): string {
  return SUBAGENT_STATE_WORD[subagentState(state)];
}

export function subagentName(agent: Pick<Subagent, "id" | "name">): string {
  return agent.name?.trim() || `Subagent ${agent.id.slice(0, 6)}`;
}

/** "Name, running" — what a tooltip and a screen reader say about one subagent. */
export function subagentLabel(agent: Subagent): string {
  return `${subagentName(agent)}, ${subagentStateWord(agent.state)}`;
}

/**
 * Running first, then failed, then finished, each keeping the order it came
 * in: the avatars that fit should be the ones worth a glance.
 */
export function orderSubagents(agents: readonly Subagent[]): Subagent[] {
  const rank = (a: Subagent) => ({ running: 0, failed: 1, done: 2 })[subagentState(a.state)];
  return agents
    .map((agent, at) => ({ agent, at }))
    .sort((a, b) => rank(a.agent) - rank(b.agent) || a.at - b.at)
    .map(({ agent }) => agent);
}

/**
 * Capitals from a name — the first letter of its first `count` words — or an
 * empty string when there is nothing to take them from.
 */
export function initials(name: string | undefined, count = 2): string {
  const words = (name ?? "").split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  return words
    .slice(0, count)
    .map((word) => Array.from(word)[0] ?? "")
    .join("")
    .toLocaleUpperCase();
}

/** The avatar group's accessible name: every subagent, not only the ones that fit. */
export function subagentsLabel(agents: readonly Subagent[]): string {
  if (agents.length === 0) return "";
  return `${agents.length} subagent${agents.length === 1 ? "" : "s"}: ${orderSubagents(agents).map(subagentLabel).join("; ")}`;
}

export function indicatorLabel(indicator: CardIndicator, attention?: Attention): string {
  switch (indicator) {
    case "permission": {
      const n = attention?.permissions ?? 0;
      return n > 1 ? `Waiting for permission (${n} requests)` : "Waiting for permission";
    }
    case "question": {
      const n = attention?.questions ?? 0;
      return n > 1 ? `Has ${n} questions for you` : "Has a question for you";
    }
    case "error":
      return "Stopped with an error";
    case "running":
      return "Running";
    default:
      return "";
  }
}

/** Everything a card says in icons, as sentences: its tooltip's second line and its screen-reader text. */
export function cardSummary(input: {
  vendorName: string;
  session: AgentSession;
  attention?: Attention;
  subagents: readonly Subagent[];
  pinned: boolean;
  age: string;
  parentTitle?: string;
}): string[] {
  const { session, attention } = input;
  const parts = [input.vendorName];
  const state = indicatorLabel(cardIndicator(session, attention), attention);
  if (state) parts.push(state);
  else if (session.status === "closed") parts.push("Closed");
  // A question waits behind a permission in the indicator; say both.
  if (attention && attention.permissions > 0 && attention.questions > 0) parts.push(indicatorLabel("question", attention));
  if (input.subagents.length) parts.push(subagentsLabel(input.subagents));
  if (input.parentTitle) parts.push(`Child of ${input.parentTitle}`);
  if (input.pinned) parts.push("Pinned");
  if (input.age) parts.push(input.age === "now" ? "Active just now" : `Active ${input.age} ago`);
  return parts;
}
