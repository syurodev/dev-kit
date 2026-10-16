import * as React from "react";
import { ChevronDown, Terminal } from "lucide-react";

import { cn } from "@/lib/utils";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { sessionTitle, showAgentSession } from "../documents";
import type { MessageSource } from "../types";
import { VendorIcon } from "../surfaces/VendorIcon";
import { MessageActions } from "../surfaces/MessageActions";

/** How much of a long message shows before "Show more". */
const COLLAPSED_LINES = 8;
const LONG_MESSAGE = 700;

/** The words a source is introduced by, for the row and for a screen reader. */
export function peerSourceLabel(source: MessageSource, liveName?: string): string {
  if (source.kind === "terminal") return "From a CLI in your terminal";
  const name = (liveName || source.name || "another session").trim();
  return `From ${name}`;
}

export type PeerMessageViewProps = {
  source: MessageSource;
  text: string;
  at?: string;
  onCopy?: (text: string) => void;
  /** The sender as the session store has it now; absent when the session is gone. */
  sender?: { title: string; agentType: string };
  /** Opens the sending session; absent leaves the label as plain text. */
  onOpenSession?: (sessionId: string) => void;
};

/**
 * A message another session (or the person's terminal) sent this one.
 *
 * It is drawn as nothing like the person's bubble: left-aligned, no fill, a
 * rule down its side, and a line saying where it came from — with the sender's
 * face and name, which open that session. The person did not write it, and the
 * agent was told so; the transcript says the same thing the agent heard.
 *
 * The name is the sender's live title when the session still exists (a renamed
 * session reads as renamed) and the title it had when sending when it does not.
 * The text is exactly what was sent, not the envelope the model received.
 */
export function PeerMessageView({ source, text, at, onCopy, sender, onOpenSession }: PeerMessageViewProps) {
  const [expanded, setExpanded] = React.useState(false);
  const long = text.length > LONG_MESSAGE || text.split("\n").length > COLLAPSED_LINES;
  const label = peerSourceLabel(source, sender?.title);
  const open = source.sessionId && sender && onOpenSession ? () => onOpenSession(source.sessionId!) : undefined;
  const vendor = sender?.agentType ?? source.agentType;

  const header = (
    <>
      {source.kind === "terminal" ? (
        <Terminal aria-hidden="true" className="size-3.5 shrink-0" />
      ) : vendor ? (
        <VendorIcon vendor={vendor} className="size-3.5 shrink-0" />
      ) : null}
      <span className="truncate">{label}</span>
    </>
  );

  return (
    <MessageActions align="start" text={text} at={at} onCopy={onCopy}>
      <div
        data-testid="peer-message"
        data-source-kind={source.kind}
        data-hop={source.hop}
        className="flex w-full max-w-4/5 flex-col gap-1 border-l-2 border-border-subtle pl-3"
      >
        <div className="flex min-w-0 items-center gap-1.5 text-label-12 text-text-secondary">
          {open ? (
            <button
              type="button"
              data-testid="peer-message-source"
              onClick={open}
              title="Open this session"
              className="flex min-w-0 cursor-pointer items-center gap-1.5 rounded-sm outline-none hover:text-text-primary focus-visible:ring-2 focus-visible:ring-border-focus/50"
            >
              {header}
            </button>
          ) : (
            <span data-testid="peer-message-source" className="flex min-w-0 items-center gap-1.5">
              {header}
              {source.sessionId && !sender ? <span className="shrink-0 text-text-tertiary">(session gone)</span> : null}
            </span>
          )}
          {source.hop > 1 ? (
            <span data-testid="peer-message-hop" className="shrink-0 text-text-tertiary">
              {`· step ${source.hop}`}
            </span>
          ) : null}
        </div>
        <p
          data-testid="peer-message-text"
          className={cn(
            "text-body-14 whitespace-pre-wrap wrap-anywhere text-text-primary select-text",
            long && !expanded && "line-clamp-8",
          )}
        >
          {text}
        </p>
        {long ? (
          <button
            type="button"
            data-testid="peer-message-toggle"
            aria-expanded={expanded}
            onClick={() => setExpanded((value) => !value)}
            className="flex w-fit cursor-pointer items-center gap-1 rounded-sm text-label-12 text-text-tertiary outline-none hover:text-text-secondary focus-visible:ring-2 focus-visible:ring-border-focus/50"
          >
            <ChevronDown aria-hidden="true" className={cn("size-3 transition-transform", expanded && "rotate-180")} />
            {expanded ? "Show less" : "Show more"}
          </button>
        ) : null}
      </div>
    </MessageActions>
  );
}

export type PeerMessageProps = Omit<PeerMessageViewProps, "sender" | "onOpenSession"> & {
  /** Opens the sending session. Defaults to the app's own way of showing one. */
  onOpenSession?: (sessionId: string) => void;
};

/** PeerMessageView with the sender read from the session store. */
export function PeerMessage({ onOpenSession = showAgentSession, ...props }: PeerMessageProps) {
  const session = useAgentSessionsStore((state) => (props.source.sessionId ? state.sessionsById[props.source.sessionId] : undefined));
  const sender = session ? { title: sessionTitle(session), agentType: session.agentType } : undefined;
  return <PeerMessageView {...props} sender={sender} onOpenSession={onOpenSession} />;
}
