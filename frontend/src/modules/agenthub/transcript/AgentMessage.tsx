import * as React from "react";
import { Bot, User } from "lucide-react";
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
  /**
   * Align Start for Agent, Align End for User.
   * Figma node: 926:126 (Agent / Message)
   */
  align?: "start" | "end";
  author?: string;
  text?: string;
  timestamp?: string;
  avatar?: React.ReactNode;
  footer?: React.ReactNode;
  children?: React.ReactNode;
  className?: string;
};

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

  return (
    <Message
      align={align}
      className={cn("w-full max-w-full", className)}
      data-testid="agent-message"
    >
      <MessageAvatar className="size-7 shrink-0 rounded-full border border-border-subtle bg-surface-raised text-text-primary self-start mt-0.5">
        {avatar ?? (isAgent ? <Bot className="size-(--icon-14)" /> : <User className="size-(--icon-14)" />)}
      </MessageAvatar>

      <MessageContent className="min-w-0 max-w-full">
        {(defaultAuthor || timestamp) && (
          <MessageHeader className="gap-2 px-1 text-caption-11-medium text-text-tertiary">
            <span>{defaultAuthor}</span>
            {timestamp && <span className="text-caption-11 text-text-tertiary">{timestamp}</span>}
          </MessageHeader>
        )}

        <BubbleContent
          className={cn(
            "rounded-lg border px-3 py-2 text-body-13 max-w-[85%] select-text whitespace-pre-wrap break-words",
            isAgent
              ? "bg-surface-raised border-border-subtle text-text-primary"
              : "bg-surface-tinted border-border-subtle text-text-primary",
          )}
        >
          {text ?? children}
        </BubbleContent>

        {footer && (
          <MessageFooter className="px-1 text-caption-11 text-text-tertiary">
            {footer}
          </MessageFooter>
        )}
      </MessageContent>
    </Message>
  );
}
