import { SessionRemoteControlItem } from "./SessionRemoteControlItem";
import * as React from "react";
import { Ban, BellOff, Bot, Copy, FileText, FolderOpen, Pencil, Pin, PinOff, Plus, Square, Trash2, Wrench } from "lucide-react";

import type { AgentSession } from "@/bridge/agenthub";
import type { Subagent } from "../types";
import { orderSubagents, subagentName, subagentStateWord } from "./model";

/**
 * What the rail's cards and groups can do, handed down by context so a card is
 * a memoised function of its own data. A prop per handler would give every card
 * a new reference whenever the owner rerendered; the owner keeps this object
 * stable instead (AgentSessionSidebar) and reads the latest handlers through
 * it.
 */
export type SessionActions = {
  open: (session: AgentSession) => void;
  openSubagent: (session: AgentSession, subagent: Subagent) => void;
  newChild?: (parent: AgentSession) => void;
  togglePin?: (sessionId: string) => void;
  environment?: (session: AgentSession) => void;
  rename?: (session: AgentSession) => void;
  copyTranscript?: (session: AgentSession) => void;
  copy?: (text: string) => void;
  cancelTurn?: (session: AgentSession) => void;
  closeSession?: (session: AgentSession) => void;
  /** Hides a failed session from Needs you without closing it; it returns by failing again. */
  dismissFailure?: (session: AgentSession) => void;
  deleteSession?: (session: AgentSession, descendants: number) => void;
  /** Folds or opens the children drawn under a session. */
  toggleBranch?: (sessionId: string) => void;
  /** Sessions that go with this one if it is deleted. */
  descendantCount: (sessionId: string) => number;
  /**
   * Rename and delete open a dialog from a menu, so they wait for the menu to
   * close and focus to go back to its trigger (heldChoice). One instance for
   * the whole rail: only one menu is open at a time.
   */
  hold: (apply: () => void) => void;
  release: (open: (apply: () => void) => void) => void;
};

export const SessionActionsContext = React.createContext<SessionActions | null>(null);

export function useSessionActions(): SessionActions {
  const actions = React.useContext(SessionActionsContext);
  if (!actions) throw new Error("useSessionActions needs an AgentSessionSidebar above it");
  return actions;
}

/** The two menu kits (right-click and the "…" button) share these rows. */
export type MenuKit = {
  Item: React.ComponentType<{
    icon?: React.ReactNode;
    variant?: "default" | "destructive";
    disabled?: boolean;
    /** Quiet text at the end of the row: a state, not a key. */
    detail?: React.ReactNode;
    onSelect?: () => void;
    children?: React.ReactNode;
  }>;
  Separator: React.ComponentType;
  Label: React.ComponentType<{ children?: React.ReactNode }>;
};

export type SubagentMenuEntry = { id: string; label: string; detail: string; agent: Subagent };

/** A session's subagents as menu rows: running first, each with its name and state. */
export function subagentMenuEntries(subagents: readonly Subagent[]): SubagentMenuEntry[] {
  return orderSubagents(subagents).map((agent) => ({
    id: agent.id,
    label: subagentName(agent),
    detail: subagentStateWord(agent.state),
    agent,
  }));
}

/**
 * The actions a session offers, in the order the tree's context menu had them.
 * Cancel and close are disabled rather than hidden when they do not apply: a
 * menu whose items move between visits is a menu you have to read every time.
 */
export function SessionMenuItems({
  kit,
  session,
  pinned,
  subagents,
  dismissable = false,
}: {
  kit: MenuKit;
  session: AgentSession;
  pinned: boolean;
  /** The session failed and nothing waits on the person in it (failureDismissable). */
  dismissable?: boolean;
  /** The agents it started inside itself: the row no longer draws them, so the menu opens them. */
  subagents: readonly Subagent[];
}) {
  const a = useSessionActions();
  const { Item, Separator, Label } = kit;
  const descendants = a.descendantCount(session.id);
  const agents = subagentMenuEntries(subagents);
  return (
    <>
      {agents.length > 0 ? (
        <>
          <Label>Subagents</Label>
          {agents.map((entry) => (
            <Item key={entry.id} icon={<Bot />} detail={entry.detail} onSelect={() => a.openSubagent(session, entry.agent)}>
              {entry.label}
            </Item>
          ))}
          <Separator />
        </>
      ) : null}
      {a.togglePin ? (
        <Item icon={pinned ? <PinOff /> : <Pin />} onSelect={() => a.togglePin?.(session.id)}>
          {pinned ? "Unpin" : "Pin to top"}
        </Item>
      ) : null}
      {a.environment ? <Item icon={<Wrench />} onSelect={() => a.hold(() => a.environment?.(session))}>Session environment…</Item> : null}
      <SessionRemoteControlItem session={session} Item={Item} />
      {a.newChild ? (
        <Item icon={<Plus />} onSelect={() => a.newChild?.(session)}>
          New child session
        </Item>
      ) : null}
      {a.rename ? (
        <Item icon={<Pencil />} onSelect={() => a.hold(() => a.rename?.(session))}>
          Rename…
        </Item>
      ) : null}

      {a.copy || a.copyTranscript ? <Separator /> : null}
      {a.copyTranscript ? (
        <Item icon={<FileText />} onSelect={() => a.copyTranscript?.(session)}>
          Copy transcript as Markdown
        </Item>
      ) : null}
      {a.copy ? (
        <>
          <Item icon={<FolderOpen />} onSelect={() => a.copy?.(session.projectPath)}>
            Copy working directory
          </Item>
          <Item icon={<Copy />} onSelect={() => a.copy?.(session.id)}>
            Copy session ID
          </Item>
        </>
      ) : null}

      {a.cancelTurn || a.closeSession || a.deleteSession || (dismissable && a.dismissFailure) ? <Separator /> : null}
      {/* Dismiss comes first: it is the gentlest of the three, and only changes
          where the row is drawn. Not offered when there is nothing to dismiss
          (a menu that moves between visits is read every time, but a row that
          could never apply is not "disabled", it is noise). */}
      {dismissable && a.dismissFailure ? (
        <Item icon={<BellOff />} onSelect={() => a.dismissFailure?.(session)}>
          Dismiss from Needs you
        </Item>
      ) : null}
      {/* Cancel stops the turn and leaves the agent running; close ends the process. */}
      {a.cancelTurn ? (
        <Item icon={<Ban />} disabled={session.status !== "running"} onSelect={() => a.cancelTurn?.(session)}>
          Cancel turn
        </Item>
      ) : null}
      {a.closeSession ? (
        <Item icon={<Square />} disabled={session.status === "closed"} onSelect={() => a.closeSession?.(session)}>
          Close session
        </Item>
      ) : null}
      {a.deleteSession ? (
        <Item
          icon={<Trash2 />}
          variant="destructive"
          onSelect={() => a.hold(() => a.deleteSession?.(session, descendants))}
        >
          {/* Says what will actually go. Deleting a parent takes its subtree,
              and finding that out afterwards is the one thing this action
              cannot undo. */}
          {descendants ? `Delete session and ${descendants} ${descendants === 1 ? "child" : "children"}` : "Delete session"}
        </Item>
      ) : null}
    </>
  );
}
