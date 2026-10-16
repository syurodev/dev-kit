import { MessageSquareShare } from "lucide-react";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview, FramedCardRow } from "@/components/ui/framed-card";
import { showAgentSession } from "../documents";
import { deliveryLabel, deliveryLine, type SentMessage } from "./messageResult";

/**
 * The `agenthub.sendMessage` result card (docs/dev-kit/tool-result-view.mdx):
 * what became of a message this session sent another one. The text it sent is
 * the tool call's own input and is not repeated here; the card says who it was
 * for, whether it landed, and opens the recipient.
 */
export function MessageSentCard({
  message,
  heading,
  onOpenSession = showAgentSession,
}: {
  message: SentMessage;
  heading?: string;
  onOpenSession?: (sessionId: string) => void;
}) {
  const refused = message.delivery === "refused";
  return (
    <FramedCard data-testid="tool-result-card" data-delivery={message.delivery}>
      <FramedCardHeader
        icon={MessageSquareShare}
        title={heading ?? (message.title ? `Message to ${message.title}` : "Message to another session")}
        meta={deliveryLabel(message.delivery)}
        action={
          message.sessionId && !refused ? (
            <FramedCardAction data-testid="message-sent-open" onClick={() => onOpenSession(message.sessionId)}>
              Open
            </FramedCardAction>
          ) : undefined
        }
      />
      <FramedCardPreview flush>
        <FramedCardRow
          label={deliveryLine(message)}
          detail={message.reason || undefined}
          trailing={message.hop > 1 ? `step ${message.hop}` : undefined}
        />
      </FramedCardPreview>
    </FramedCard>
  );
}
