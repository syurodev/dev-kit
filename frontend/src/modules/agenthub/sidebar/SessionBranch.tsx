import * as React from "react";

import { TREE_ELBOW_RADIUS, TREE_GUIDE } from "@/components/tree-guides";
import type { Subagent } from "../types";
import { AgentSessionCard } from "./AgentSessionCard";
import { branchHint, branchOpen, indented, type BranchSignals, type SessionNode } from "./tree";

const NO_SUBAGENTS: readonly Subagent[] = [];

/**
 * The geometry the guides hang off, in px, from the rail's card (50px, 8px of
 * padding, a 30px face): the face's centre is where a line leaves the parent,
 * and a child starts one indent further in. The elbow runs from the line to a
 * few px short of the child's face, at the height of its middle.
 */
const CARD_HEIGHT = 50;
const FACE_BOTTOM = 40;
const LINE_X = 23;
const INDENT = 28;
const ELBOW_WIDTH = 10;
const ROW_GAP = 2;

export type BranchContext = {
  activeId?: string;
  pinned: ReadonlySet<string>;
  folded: ReadonlySet<string>;
  /** Branches that stay open for the session on screen or a pinned one. */
  held: ReadonlySet<string>;
  subagents: Record<string, Subagent[]>;
  peerUnread: Record<string, number>;
  signals: Record<string, BranchSignals>;
  titles: ReadonlyMap<string, string>;
  /** A workspace of several folders says which one each row works in. */
  folderOf: (projectPath: string) => string | null;
  now: number;
};

/** The guide for one child: the line down its parent's side and the elbow into its row. */
function Guides({ first, last }: { first: boolean; last: boolean }) {
  const top = first ? -(CARD_HEIGHT - FACE_BOTTOM) : -ROW_GAP;
  const x = LINE_X - INDENT;
  return (
    <span aria-hidden="true" data-testid="agent-branch-guide">
      {last ? null : (
        <span style={{ position: "absolute", left: x, top, bottom: -ROW_GAP, borderLeft: TREE_GUIDE }} />
      )}
      <span
        style={{
          position: "absolute",
          left: x,
          top,
          width: ELBOW_WIDTH,
          height: CARD_HEIGHT / 2 - top,
          borderLeft: TREE_GUIDE,
          borderBottom: TREE_GUIDE,
          borderBottomLeftRadius: TREE_ELBOW_RADIUS,
        }}
      />
    </span>
  );
}

/**
 * One session of the rail and, under it, its children: the list item holds the
 * card and a nested list, so a screen reader hears "list, 3 items" inside the
 * parent rather than a flat run of rows. Children are indented and joined to
 * the parent by a line with an elbow into each row; below INDENT_LEVELS they
 * keep the last indent and draw no line (aria-level still says the real one).
 */
export function SessionBranch({
  node,
  first,
  last,
  guided,
  ctx,
}: {
  node: SessionNode;
  first: boolean;
  last: boolean;
  /** Drawn indented under its parent, with the parent's line. */
  guided: boolean;
  ctx: BranchContext;
}) {
  const { session } = node;
  const listId = React.useId();
  const held = ctx.held.has(session.id);
  const open = node.children.length > 0 && branchOpen(session.id, ctx.folded, held);
  const hint = branchHint(ctx.signals[session.id], open);
  const indent = indented(node.level);

  return (
    <div
      role="listitem"
      aria-level={node.level + 1}
      data-testid="agent-session-node"
      data-level={node.level}
      className="relative"
    >
      <AgentSessionCard
        inTree
        nested={node.level > 0}
        session={session}
        selected={session.id === ctx.activeId}
        // Anything waiting on the person is drawn in Needs you, not here.
        ask={null}
        folder={ctx.folderOf(session.projectPath)}
        pinned={ctx.pinned.has(session.id)}
        subagents={ctx.subagents[session.id] ?? NO_SUBAGENTS}
        peerUnread={ctx.peerUnread[session.id]}
        parentTitle={session.parentSessionId ? ctx.titles.get(session.parentSessionId) : undefined}
        now={ctx.now}
        childCount={node.descendants}
        branchOpen={open}
        branchHeld={held}
        hintKind={hint?.kind}
        hintCount={hint?.count}
        branchId={listId}
      />
      {/* After the card, so the elbow paints over its ground when selected. */}
      {guided ? <Guides first={first} last={last} /> : null}
      {open ? (
        <div
          id={listId}
          role="list"
          data-testid="agent-branch-list"
          aria-label={`Sessions started by ${ctx.titles.get(session.id) ?? "this session"}`}
          className="relative mt-0.5 flex flex-col gap-0.5"
          style={indent ? { paddingLeft: INDENT } : undefined}
        >
          {node.children.map((child, at) => (
            <SessionBranch
              key={child.session.id}
              node={child}
              first={at === 0}
              last={at === node.children.length - 1}
              guided={indent}
              ctx={ctx}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}
