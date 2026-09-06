import * as React from "react";

import { cn } from "@/lib/utils";
import {
  Message,
  MessageAvatar,
  MessageContent,
  MessageHeader,
  MessageFooter,
} from "@/components/ui/message";
import { BubbleContent } from "@/components/ui/bubble";

export type AgentMessageProps = {
  /** Start = the agent, End = you. Figma `926:126`. */
  align?: "start" | "end";
  author?: string;
  text?: string;
  timestamp?: string;
  avatar?: React.ReactNode;
  footer?: React.ReactNode;
  children?: React.ReactNode;
  className?: string;
};

/**
 * Agent / Message — Figma `926:126`, variants `929:145` Start/Wide and
 * `929:157` End/Wide.
 *
 * Figma's note: "Follows the shadcn Message anatomy so the Figma layers map 1:1
 * onto the code: MessageAvatar + MessageContent(MessageHeader, Bubble >
 * BubbleContent, MessageFooter), with align start for the agent and end for
 * you." The primitives already carry the reversal, the avatar's self-end and
 * the footer's justify-end, so this file only supplies the tones.
 *
 * The avatar is the author's initial on a 24px round, not a generic Bot/User
 * pair: with several agents in a swarm, two Bot glyphs are the same glyph, and
 * the name is the only thing that tells refund-worker from docs-sweep. Yours
 * takes action/primary, the way it does everywhere the accent means "you".
 *
 * The bubble has no border in Figma — the fill is what separates the two
 * speakers (surface/raised for the agent, surface/tinted for you), and a
 * hairline on top of that reads as a card rather than as speech.
 */
export function AgentMessage({
  align = "start",
  author,
  text,
  timestamp,
  avatar,
  footer,
  children,
  className,
}: AgentMessageProps) {
  const isAgent = align === "start";
  const defaultAuthor = author ?? (isAgent ? "Agent" : "You");
  const initial = defaultAuthor.trim().charAt(0) || (isAgent ? "a" : "y");

  return (
    <Message
      align={align}
      className={cn("w-full max-w-full items-end", className)}
      data-testid="agent-message"
    >
      <MessageAvatar
        className={cn(
          "size-6 min-w-6 rounded-full text-caption-11-medium",
          isAgent
            ? "bg-(--state-soft) text-text-secondary"
            : "bg-action-primary text-action-primary-foreground",
        )}
      >
        {avatar ?? <span aria-hidden="true">{initial}</span>}
      </MessageAvatar>

      <MessageContent className="min-w-0 max-w-full gap-1">
        <MessageHeader className="gap-2 px-0 text-caption-11-medium text-text-tertiary">
          <span>{defaultAuthor}</span>
          {timestamp ? <span className="text-caption-11">{timestamp}</span> : null}
        </MessageHeader>

        <BubbleContent
          className={cn(
            "max-w-[85%] rounded-[10px] px-3 py-2 text-body-12 text-text-primary",
            "whitespace-pre-wrap break-words select-text",
            isAgent ? "bg-surface-raised" : "bg-surface-tinted",
          )}
        >
          {text ?? children}
        </BubbleContent>

        {footer ? (
          <MessageFooter className="gap-3 px-0 text-caption-11 text-text-tertiary">
            {footer}
          </MessageFooter>
        ) : null}
      </MessageContent>
    </Message>
  );
}
