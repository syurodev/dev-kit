import { MessageSquareShare } from "lucide-react";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { sessionTitle, showAgentSession } from "../documents";
import { PeerParty, StatusChip } from "../transcript/peerParts";
import { deliveryLabel, deliveryLine, type SentMessage } from "./messageResult";

/**
 * The `agenthub.sendMessage` result card (docs/dev-kit/tool-result-view.mdx):
 * what became of a message this session sent another one. The text it sent is
 * the tool call's own input and is not repeated here; the card says who it was
 * for — the recipient's face and current title, as the receiving transcript
 * names its sender — whether it landed (a chip, then a sentence), and opens
 * the recipient. It is the same framed card, party and chip the received
 * message uses (transcript/PeerMessage).
 *
 * `recipient` is what the session store knows of it now; a caller with no
 * store (the gallery) passes it, and a session that is gone keeps the title
 * the tool answered with.
 */
export function MessageSentCard({
  message,
  heading,
  recipient,
  onOpenSession = showAgentSession,
}: {
  message: SentMessage;
  heading?: string;
  recipient?: { title: string; agentType: string };
  onOpenSession?: (sessionId: string) => void;
}) {
  const stored = useAgentSessionsStore((state) => (message.sessionId ? state.sessionsById[message.sessionId] : undefined));
  const live = recipient ?? (stored ? { title: sessionTitle(stored), agentType: stored.agentType } : undefined);
  const refused = message.delivery === "refused";
  const open = message.sessionId && !refused ? () => onOpenSession(message.sessionId) : undefined;
  const title = live?.title || message.title;
  return (
    <FramedCard data-testid="tool-result-card" data-delivery={message.delivery} className="max-w-160">
      <FramedCardHeader
        icon={MessageSquareShare}
        title={
          heading ?? (title ? (
            <PeerParty prefix="Message to" title={title} vendor={live?.agentType} testId="message-sent-recipient" />
          ) : (
            "Message to another session"
          ))
        }
        meta={
          <StatusChip tone={message.delivery} testId="message-sent-delivery">
            {deliveryLabel(message.delivery)}
          </StatusChip>
        }
        action={
          open ? (
            <FramedCardAction data-testid="message-sent-open" aria-label={`Open ${title || "the session"}`} onClick={open}>
              Open
            </FramedCardAction>
          ) : undefined
        }
      />
      <FramedCardPreview className="gap-0.5">
        <p className="text-body-13 text-text-primary">{deliveryLine(message)}</p>
        {message.reason ? <p className="text-caption-11 wrap-anywhere text-text-secondary">{message.reason}</p> : null}
        {message.hop > 1 ? <p className="text-caption-11 text-text-tertiary tabular-nums">{`step ${message.hop}`}</p> : null}
      </FramedCardPreview>
    </FramedCard>
  );
}
