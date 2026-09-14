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
import { VendorIcon } from "../surfaces/VendorIcon";

export type AgentMessageProps = {
  /** Start = the agent, End = you. Figma `926:126`. */
  align?: "start" | "end";
  author?: string;
  /** Which vendor is speaking, for the avatar mark. */
  vendor?: string;
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
 * The agent's avatar is its vendor mark; yours is your initial on
 * action/primary, the way the accent means "you" everywhere else. Initials on
 * both sides read as two people, and an untitled session yields "S" for
 * "Session".
 *
 * The bubble has no border: the fill separates the speakers (surface/raised for
 * the agent, surface/tinted for you), and a hairline over that reads as a card
 * rather than as speech.
 */
export function AgentMessage({
  align = "start",
  author,
  vendor,
  text,
  timestamp,
  avatar,
  footer,
  children,
  className,
}: AgentMessageProps) {
  const isAgent = align === "start";
  const defaultAuthor = author ?? (isAgent ? "Agent" : "You");
  const initial = defaultAuthor.trim().charAt(0) || "y";

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
        {avatar ??
          (isAgent ? (
            <VendorIcon vendor={vendor} className="size-3.5" />
          ) : (
            <span aria-hidden="true">{initial}</span>
          ))}
      </MessageAvatar>

      <MessageContent className="min-w-0 max-w-full gap-1">
        <MessageHeader className="gap-2 px-0 text-caption-11-medium text-text-tertiary">
          <span>{defaultAuthor}</span>
          {timestamp ? <span className="text-caption-11">{timestamp}</span> : null}
        </MessageHeader>

        <BubbleContent
          className={cn(
            "max-w-[85%] rounded-lg px-4 py-3 text-body-16 text-text-primary",
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
