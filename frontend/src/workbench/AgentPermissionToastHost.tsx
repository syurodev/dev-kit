import { useEffect, useState } from "react";

import type { AgentSession, PendingElicitation, PendingPermission } from "@/bridge/agenthub";
import { PermissionPresence } from "@/modules/agenthub/surfaces/permissionCard";
import { useResolvePermission } from "@/modules/agenthub/permissions";
import { useRespondElicitation } from "@/modules/agenthub/elicitations";
import { AgentPermissionToast } from "@/modules/agenthub/surfaces/AgentPermissionToast";
import { AgentQuestionToast } from "@/modules/agenthub/surfaces/AgentQuestionToast";
import { useAgentSessionList, useAgentSessionsStore } from "@/stores/agentSessions";
import { SHELL_FLOAT_ATTR } from "@/lib/shellFloat";

/**
 * Hosts the Agent / Permission Toast (Figma 924:152) at the shell level, and
 * the question toast in the same corner.
 *
 * Appears floating bottom-right ONLY when no surface is already asking inline:
 * the Agent Hub module and the rail both put the card in the conversation the
 * request came out of, which is where the evidence for the answer is. The toast
 * is for when that conversation is not on screen — rail closed, another module,
 * or either of them showing a different session.
 *
 * One slot, not two stacked toasts. When a permission and a question are both
 * waiting, the permission is shown: it is the one with a deadline, and a
 * question left for a moment loses nothing.
 *
 * "Open rail" reaches the full transcript before granting anything persistent;
 * "Allow once" approves a single turn.
 */
export type AgentPermissionToastHostProps = {
  activeModuleId?: string | null;
  pending?: PendingPermission | null;
  session?: AgentSession | null;
  chatOpen?: boolean;
};

type Shown =
  | { kind: "permission"; item: PendingPermission }
  | { kind: "question"; item: PendingElicitation };

export function AgentPermissionToastHost({
  activeModuleId,
  pending: propPending,
  session: propSession,
  chatOpen: propChatOpen,
}: AgentPermissionToastHostProps = {}) {
  const storePending = useAgentSessionsStore((s) => s.pending);
  const storeQueueLength = useAgentSessionsStore((s) => s.pendingQueue.length);
  const storeQuestions = useAgentSessionsStore((s) => s.elicitationQueue);
  const storeSessions = useAgentSessionList();
  const storeActiveSessionId = useAgentSessionsStore((s) => s.activeSessionId);
  const storeChatOpen = useAgentSessionsStore((s) => s.chatOpen);
  const setChatOpen = useAgentSessionsStore((s) => s.setChatOpen);
  const setActiveSessionId = useAgentSessionsStore((s) => s.setActiveSessionId);

  // In SSR / bun test without DOM window, Zustand getServerSnapshot returns immutable initial state,
  // so we fallback to getState() when window is undefined.
  const isSSR = typeof window === "undefined";
  const pending =
    propPending !== undefined
      ? propPending
      : isSSR
        ? useAgentSessionsStore.getState().pending
        : storePending;
  const questions = isSSR ? useAgentSessionsStore.getState().elicitationQueue : storeQuestions;
  const sessions = isSSR
    ? useAgentSessionsStore.getState().sessionIds.map((id) => useAgentSessionsStore.getState().sessionsById[id])
    : storeSessions;
  const activeSessionId = isSSR ? useAgentSessionsStore.getState().activeSessionId : storeActiveSessionId;
  const chatOpen =
    propChatOpen !== undefined
      ? propChatOpen
      : isSSR
        ? useAgentSessionsStore.getState().chatOpen
        : storeChatOpen;

  // Both inline surfaces read the same activeSessionId, and both fall back to
  // the first session when nothing is selected — so does this, or a request on
  // that session would be asked inline AND floated here.
  const shownSessionId = activeSessionId ?? sessions[0]?.id ?? null;
  const askedInline = (sessionId: string) =>
    shownSessionId === sessionId && (chatOpen || activeModuleId === "agenthub");

  const permissionShowing = pending != null && !askedInline(pending.sessionId);
  // The oldest question nobody is looking at. The inline card only ever takes
  // the shown session's, so a second session's question still needs the corner.
  const question = permissionShowing ? null : (questions.find((q) => !askedInline(q.sessionId)) ?? null);
  const current: Shown | null = permissionShowing
    ? { kind: "permission", item: pending }
    : question
      ? { kind: "question", item: question }
      : null;

  // Before the early return: hooks cannot be skipped, and null is a fine
  // argument — there is simply nothing to answer.
  const { busy, resolve } = useResolvePermission(permissionShowing ? pending : null);
  const { busy: questionBusy, respond } = useRespondElicitation(question);

  // The toast outlives its request by one exit animation, by holding on to the
  // request it was showing. Without this the host returned null in the same
  // render the answer landed in, taking AnimatePresence down with it before it
  // could animate anything — the card blinked out mid-gesture.
  //
  // Held, not a boolean: the leaving card still has to draw the agent, the
  // command and the clock while it goes, and `pending` is already the next
  // request or nothing at all.
  const [held, setHeld] = useState<Shown | null>(null);
  const currentId = current?.item.id;
  useEffect(() => {
    if (current) setHeld(current);
    // Keyed by id: `current` is a fresh object every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentId, current?.kind]);

  // Nothing rendered until a request has actually been shown, so a window with
  // nothing pending still renders nothing at all.
  const card = current ?? held;
  if (!card) return null;

  const session = propSession ?? sessions.find((s) => s.id === card.item.sessionId) ?? null;

  function openRailOn(sessionId: string) {
    setActiveSessionId(sessionId);
    setChatOpen(true);
  }

  return (
    // Placement is the host's job, not the card's. Figma 924:152 draws a 420
    // card; "bottom-right, floating over the module" is in its description, and
    // a card that positions itself cannot be put anywhere else — including the
    // gallery, where it would fly to the corner of the page.
    //
    // z-60 is one step over the z-50 that dialogs, sheets and menus share: a
    // request that fails closed must stay answerable with Settings open. The
    // shell-float mark keeps that dialog from reading the click as "dismiss",
    // and pointer-events-auto survives the body lock a modal puts on.
    <div {...{ [SHELL_FLOAT_ATTR]: "" }} className="pointer-events-none fixed right-4 bottom-4 z-60">
      <PermissionPresence
        pendingId={current ? current.item.id : null}
        variant="toast"
        className="pointer-events-auto"
        onExitComplete={() => setHeld(null)}
      >
        {card.kind === "permission" ? (
          <AgentPermissionToast
            pending={card.item}
            session={session}
            onResolve={resolve}
            onOpenRail={() => openRailOn(card.item.sessionId)}
            busy={busy}
            // pendingQueue holds the request on screen too; the pill counts the rest.
            queued={Math.max(0, (isSSR ? useAgentSessionsStore.getState().pendingQueue.length : storeQueueLength) - 1)}
          />
        ) : (
          <AgentQuestionToast
            pending={card.item}
            session={session}
            onRespond={respond}
            onAnswer={() => openRailOn(card.item.sessionId)}
            busy={questionBusy}
            queued={Math.max(0, questions.length - 1)}
          />
        )}
      </PermissionPresence>
    </div>
  );
}
