import * as React from "react";
import { ChevronDown } from "lucide-react";

import { FramedCard, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { cn } from "@/lib/utils";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { sessionTitle, showAgentSession } from "../documents";
import type { MessageSource } from "../types";
import { MessageActions } from "../surfaces/MessageActions";
import { PeerParty, StatusChip } from "./peerParts";

/** How many lines show before "Show more". */
const COLLAPSED_LINES = 3;
/** Before the text is measured (and where nothing is): a message this long or this tall is taken to overflow. */
const LONG_MESSAGE = 220;

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
 * Whether a clamped paragraph is cut. Measured (it depends on the width the
 * rail or the column gives it) and kept while it is open, so "Show less"
 * stays where it was. Until measured — and in a string render, where nothing
 * is — the length of the text decides.
 */
function useClampOverflow(ref: React.RefObject<HTMLElement | null>, text: string, expanded: boolean): boolean {
  const guess = text.length > LONG_MESSAGE || text.split("\n").length > COLLAPSED_LINES;
  const [measured, setMeasured] = React.useState<boolean | null>(null);
  React.useLayoutEffect(() => {
    const el = ref.current;
    if (!el || expanded) return;
    const measure = () => setMeasured(el.scrollHeight > el.clientHeight + 1);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref, text, expanded]);
  return measured ?? guess;
}

/**
 * A message another session (or the person's terminal) sent this one.
 *
 * It is drawn as nothing like the person's bubble: left-aligned on the framed
 * card every tool result uses (raised frame, inset body), under a header that
 * says where it came from — the sender's face and name, which open that
 * session. The person did not write it, and the agent was told so; the
 * transcript says the same thing the agent heard.
 *
 * The name is the sender's live title when the session still exists (a renamed
 * session reads as renamed) and the title it had when sending when it does not.
 * The text is exactly what was sent, not the envelope the model received, and
 * is clamped to three lines: a status report of twenty lines is a click away,
 * not a wall in the middle of the conversation.
 */
export function PeerMessageView({ source, text, at, onCopy, sender, onOpenSession }: PeerMessageViewProps) {
  const [expanded, setExpanded] = React.useState(false);
  const paragraph = React.useRef<HTMLParagraphElement>(null);
  const textId = React.useId();
  const long = useClampOverflow(paragraph, text, expanded);
  const terminal = source.kind === "terminal";
  const name = terminal ? "a CLI in your terminal" : (sender?.title || source.name || "another session").trim();
  const open = source.sessionId && sender && onOpenSession ? () => onOpenSession(source.sessionId!) : undefined;
  const vendor = sender?.agentType ?? source.agentType;

  return (
    <MessageActions align="start" text={text} at={at} onCopy={onCopy}>
      <FramedCard data-testid="peer-message" data-source-kind={source.kind} data-hop={source.hop} className="max-w-160">
        <FramedCardHeader
          title={
            <PeerParty
              prefix="From"
              title={name}
              vendor={vendor}
              terminal={terminal}
              onOpen={open}
              testId="peer-message-source"
              suffix={source.sessionId && !sender ? "(session gone)" : undefined}
            />
          }
          meta={source.hop > 1 ? <StatusChip testId="peer-message-hop">{`step ${source.hop}`}</StatusChip> : undefined}
        />
        <FramedCardPreview>
          <p
            ref={paragraph}
            id={textId}
            data-testid="peer-message-text"
            className={cn("text-body-14 whitespace-pre-wrap wrap-anywhere text-text-primary select-text", long && !expanded && "line-clamp-3")}
          >
            {text}
          </p>
          {long ? (
            <button
              type="button"
              data-testid="peer-message-toggle"
              aria-expanded={expanded}
              aria-controls={textId}
              onClick={() => setExpanded((value) => !value)}
              className="flex w-fit cursor-pointer items-center gap-1 rounded-sm text-label-12 text-text-tertiary outline-none hover:text-text-secondary focus-visible:ring-2 focus-visible:ring-border-focus/50"
            >
              <ChevronDown aria-hidden="true" className={cn("size-3 transition-transform motion-reduce:transition-none", expanded && "rotate-180")} />
              {expanded ? "Show less" : "Show more"}
            </button>
          ) : null}
        </FramedCardPreview>
      </FramedCard>
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
