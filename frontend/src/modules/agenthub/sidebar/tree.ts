import type { AgentSession } from "@/bridge/agenthub";
import { isWorking } from "../sessionActivity";
import { failureDismissed, type DismissedFailures } from "./dismissed";
import { attentionKind, compareRecent, partitionGroup, type Attention, type GroupView, type PartitionOptions } from "./model";

/**
 * The parent → child half of the Agent Hub rail. A group is a list of sessions
 * (model.ts groupSessions decides which); this turns that list into a forest so
 * a child draws under the session that started it. Pure: no React, no store.
 *
 * Three rules hold everywhere here:
 *  - a session whose parent is not in the list (deleted, in another group,
 *    hidden as closed, waiting in Needs you) is a ROOT, never dropped;
 *  - a cycle in the data cannot loop or lose a session: the newest member of
 *    an unreachable cycle becomes a root and its parent link is ignored;
 *  - a tree is ONE entry of its group when the group is cut to the five most
 *    recent, however many sessions it holds.
 */

/** How many levels of children are indented; deeper ones keep the last indent. */
export const INDENT_LEVELS = 2;

export type SessionNode = {
  session: AgentSession;
  /** 0 for a root. The real depth in the forest, whatever is drawn indented. */
  level: number;
  children: SessionNode[];
  /** Sessions below this one in the forest, itself excluded. */
  descendants: number;
  /** The newest `updatedAt` in the subtree: what orders a root among its siblings. */
  recent: number;
};

/** Children are drawn indented under a node at `level` while that is within the cap. */
export function indented(level: number): boolean {
  return level < INDENT_LEVELS;
}

/**
 * The forest of `sessions`. Roots and every node's children are newest first
 * (compareRecent), so the order of a group does not depend on the order the
 * list arrived in.
 */
export function buildForest(sessions: readonly AgentSession[]): SessionNode[] {
  const ids = new Set(sessions.map((s) => s.id));
  const kids = new Map<string, AgentSession[]>();
  const roots: AgentSession[] = [];
  for (const session of sessions) {
    const parent = session.parentSessionId;
    if (parent && parent !== session.id && ids.has(parent)) {
      const list = kids.get(parent) ?? [];
      list.push(session);
      kids.set(parent, list);
    } else {
      roots.push(session);
    }
  }

  const placed = new Set<string>();
  const build = (session: AgentSession, level: number): SessionNode => {
    placed.add(session.id);
    const children = (kids.get(session.id) ?? [])
      .filter((child) => !placed.has(child.id))
      .sort(compareRecent)
      .map((child) => build(child, level + 1));
    let descendants = 0;
    let recent = session.updatedAt ?? 0;
    for (const child of children) {
      descendants += 1 + child.descendants;
      recent = Math.max(recent, child.recent);
    }
    return { session, level, children, descendants, recent };
  };

  const out = roots.sort(compareRecent).map((root) => build(root, 0));
  // What the roots did not reach sits in a cycle (a → b → a): break it at the
  // newest member, in recency order so the result is the same on every render.
  for (const session of [...sessions].sort(compareRecent)) {
    if (!placed.has(session.id)) out.push(build(session, 0));
  }
  return out;
}

/** The node and everything under it, depth first. */
export function flattenNode(node: SessionNode): SessionNode[] {
  return [node, ...node.children.flatMap(flattenNode)];
}

/** Ids of the nodes on the path from a root down to the first node holding `id`, `id` excluded. */
export function ancestorsOf(roots: readonly SessionNode[], id: string): string[] {
  const walk = (node: SessionNode, path: string[]): string[] | null => {
    if (node.session.id === id) return path;
    for (const child of node.children) {
      const found = walk(child, [...path, node.session.id]);
      if (found) return found;
    }
    return null;
  };
  for (const root of roots) {
    const found = walk(root, []);
    if (found) return found;
  }
  return [];
}

export type ForestOptions = {
  activeId?: string;
  /** Pinned ids, in pin order. */
  pinnedIds?: readonly string[];
  limit?: number;
};

/**
 * Which trees a group draws and which fold into its stack of older ones.
 *
 * partitionGroup's rule, applied to roots: a tree is as recent as its newest
 * session, and a tree is drawn though old when it holds the session on screen
 * or a pinned one (a waiting session is drawn in Needs you, not here). Only a
 * root pinned itself is moved up front; a root drawn because a child is pinned
 * keeps the place its age gives it.
 */
export function partitionForest(
  roots: readonly SessionNode[],
  options: ForestOptions & Pick<PartitionOptions, "waiting"> = {},
): { shown: SessionNode[]; stacked: SessionNode[] } {
  const { activeId, pinnedIds = [], limit } = options;
  const byRoot = new Map(roots.map((root) => [root.session.id, root]));
  const pinned = new Set(pinnedIds);
  const holds = new Map(
    roots.map((root) => {
      const all = flattenNode(root);
      return [root.session.id, all.some((n) => n.session.id === activeId || pinned.has(n.session.id) || options.waiting?.(n.session))] as const;
    }),
  );
  const view: GroupView = partitionGroup(
    // The root stands for its tree: the tree's age, and its own id.
    roots.map((root) => ({ ...root.session, updatedAt: root.recent })),
    {
      limit,
      pinnedIds: pinnedIds.filter((id) => byRoot.has(id)),
      // "waiting" is partitionGroup's name for an exception; here it is any
      // reason to keep a tree drawn.
      waiting: (root) => holds.get(root.id) ?? false,
    },
  );
  const node = (s: AgentSession) => byRoot.get(s.id) as SessionNode;
  return { shown: view.shown.map(node), stacked: view.stacked.map(node) };
}

/** Whether a node's children are drawn: the person's fold, unless something in it must stay in view. */
export function branchOpen(id: string, folded: ReadonlySet<string>, held: boolean): boolean {
  return held || !folded.has(id);
}

/** Ids of the branches that must stay open: those above the session on screen or a pinned one. */
export function heldBranches(roots: readonly SessionNode[], activeId: string | undefined, pinnedIds: readonly string[]): Set<string> {
  const held = new Set<string>();
  for (const id of [activeId, ...pinnedIds]) {
    if (!id) continue;
    for (const ancestor of ancestorsOf(roots, id)) held.add(ancestor);
  }
  return held;
}

/** A branch always draws at least this many of its children (more when more are running). */
export const MIN_VISIBLE_CHILDREN = 3;

/**
 * Which children of one node a branch draws and which fold into its "more" row.
 *
 * The limit is the larger of MIN_VISIBLE_CHILDREN and the number of running
 * children. Every running child is drawn; what is left of the limit goes to the
 * newest of the rest (`children` arrives newest first, as buildForest sorts it).
 * Any child that is, or holds in its subtree, the session on screen or a pinned
 * one is an exception: it is drawn too, even past the limit. Both lists keep
 * that order. Level-agnostic: it takes any node's children.
 */
export function visibleChildren(
  children: readonly SessionNode[],
  options: { activeId?: string; pinned?: ReadonlySet<string> } = {},
): { shown: SessionNode[]; hidden: SessionNode[] } {
  const { activeId, pinned } = options;
  const running = (node: SessionNode) => isWorking(node.session);
  const limit = Math.max(MIN_VISIBLE_CHILDREN, children.filter(running).length);
  if (children.length <= limit) return { shown: [...children], hidden: [] };

  const holds = (node: SessionNode) =>
    flattenNode(node).some((n) => n.session.id === activeId || (pinned?.has(n.session.id) ?? false));
  const chosen = new Set<SessionNode>(children.filter(running));
  for (const node of children) {
    if (chosen.size >= limit) break;
    chosen.add(node);
  }
  // Exceptions come on top of the limit, so keeping one in view never costs a newer child its place.
  for (const node of children) if (holds(node)) chosen.add(node);
  return {
    shown: children.filter((node) => chosen.has(node)),
    hidden: children.filter((node) => !chosen.has(node)),
  };
}

// ---------------------------------------------------------------------------
// What is going on below a session

/** What the sessions below one session are doing, counted over EVERY session, drawn here or not. */
export type BranchSignals = { running: number; waiting: number; failed: number; unread: number };

const NO_SIGNALS: BranchSignals = { running: 0, waiting: 0, failed: 0, unread: 0 };

/**
 * For each session, what its descendants are doing. It reads the whole list,
 * not the drawn group: a child waiting on the person is drawn in Needs you and
 * a closed one may be hidden, and the parent is where someone looks for them.
 * Closed sessions count for nothing (they do nothing). `unread` is messages
 * from other sessions the person has not seen (peerUnread), the one unread
 * mark Agent Hub keeps.
 */
export function branchSignals(
  sessions: readonly AgentSession[],
  attention: Record<string, Attention>,
  peerUnread: Record<string, number>,
  dismissed?: DismissedFailures,
): Record<string, BranchSignals> {
  const kids = new Map<string, AgentSession[]>();
  for (const session of sessions) {
    if (!session.parentSessionId || session.parentSessionId === session.id) continue;
    const list = kids.get(session.parentSessionId) ?? [];
    list.push(session);
    kids.set(session.parentSessionId, list);
  }
  const out: Record<string, BranchSignals> = {};
  const own = (s: AgentSession): BranchSignals => {
    if (s.status === "closed") return NO_SIGNALS;
    const waiting = attentionKind(s, attention[s.id]) !== null;
    return {
      running: isWorking(s) && !waiting ? 1 : 0,
      waiting: waiting ? 1 : 0,
      // A failure the person dismissed no longer marks the parent.
      failed: s.status === "error" && !failureDismissed(s, dismissed) ? 1 : 0,
      unread: (peerUnread[s.id] ?? 0) > 0 ? 1 : 0,
    };
  };
  const walk = (id: string, seen: Set<string>): BranchSignals => {
    const total = { ...NO_SIGNALS };
    for (const child of kids.get(id) ?? []) {
      if (seen.has(child.id)) continue;
      seen.add(child.id);
      const mine = own(child);
      const below = walk(child.id, seen);
      total.running += mine.running + below.running;
      total.waiting += mine.waiting + below.waiting;
      total.failed += mine.failed + below.failed;
      total.unread += mine.unread + below.unread;
    }
    return total;
  };
  for (const [parent] of kids) out[parent] = walk(parent, new Set([parent]));
  return out;
}

export type BranchHint = { kind: "waiting" | "failed" | "running" | "unread"; count: number };

/**
 * The one mark a parent row carries for what is below it.
 *
 * A child waiting or failed is drawn in Needs you, never in the tree, so it is
 * marked on the parent whether the branch is open or not. Running and unread
 * children are drawn under an open parent and say it themselves; only a folded
 * parent speaks for them. Waiting beats failed beats running beats unread: the
 * order of what costs the person something if it is missed.
 */
export function branchHint(signals: BranchSignals | undefined, open: boolean): BranchHint | null {
  if (!signals) return null;
  if (signals.waiting > 0) return { kind: "waiting", count: signals.waiting };
  if (signals.failed > 0) return { kind: "failed", count: signals.failed };
  if (open) return null;
  if (signals.running > 0) return { kind: "running", count: signals.running };
  if (signals.unread > 0) return { kind: "unread", count: signals.unread };
  return null;
}

/** The hint as a sentence, for a screen reader and the tooltip. */
export function branchHintLabel(hint: BranchHint | null): string {
  if (!hint) return "";
  const n = hint.count;
  switch (hint.kind) {
    case "waiting":
      return n === 1 ? "A child session is waiting for you" : `${n} child sessions are waiting for you`;
    case "failed":
      return n === 1 ? "A child session stopped with an error" : `${n} child sessions stopped with an error`;
    case "running":
      return n === 1 ? "A child session is running" : `${n} child sessions are running`;
    case "unread":
      return n === 1 ? "A child session has a new message" : `${n} child sessions have new messages`;
  }
}

/** "3 child sessions", as a screen reader says the count on a parent row. */
export function childCountLabel(n: number): string {
  return `${n} ${n === 1 ? "child session" : "child sessions"}`;
}
