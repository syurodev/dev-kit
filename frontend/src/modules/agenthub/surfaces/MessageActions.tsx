import * as React from "react";
import { Check, Copy, Pencil } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";

export type MessageActionsProps = {
  /** The message itself. */
  children: React.ReactNode;
  /** Which edge the row lines up with — yours on the right, the agent's on the left. */
  align: "start" | "end";
  /** What Copy and Edit act on: the message as it was written. */
  text: string;
  /** When it was said, as the ISO timestamp the transcript row carries. */
  at?: string;
  /** "1.2s · $0.03" for a reply that carried numbers; opens the inspector. */
  meta?: string;
  onDetails?: () => void;
  onCopy?: (text: string) => void;
  /** Your own prompt only: puts its text back in the composer. */
  onEdit?: (text: string) => void;
};

/** "2:56 PM" in the viewer's own locale; nothing for a row without a clock. */
function timeOf(at?: string): string {
  const ms = Date.parse(at ?? "");
  return Number.isFinite(ms) ? new Date(ms).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }) : "";
}

/**
 * A message's time and its quick actions, in a row right under it that shows
 * while the message is hovered or has keyboard focus inside.
 *
 * Inline, not a floating card: the row reads as part of the message it acts on,
 * lines up with that message's edge, and a pointer moving from the text to Copy
 * never crosses a gap where a card could close. It is also one path for mouse
 * and keyboard alike — Tab reaches the buttons in page order and focus reveals
 * the row, where a portalled card closed as soon as focus left the message.
 *
 * The row always takes its height and only its opacity changes. Showing it by
 * display would push everything below down by a row the moment the pointer
 * arrived — the transcript jumping under the pointer, which then hovers the
 * next message instead. Hidden rows are still not a line of chrome under every
 * message: they are empty space until pointed at.
 */
export function MessageActions({ children, align, text, at, meta, onDetails, onCopy, onEdit }: MessageActionsProps) {
  const [copied, setCopied] = React.useState(false);
  const time = timeOf(at);
  if (!time && !meta && !onCopy && !onEdit) return <>{children}</>;

  return (
    <div
      data-slot="message"
      className={cn("group/message flex w-full flex-col gap-1", align === "end" ? "items-end" : "items-start")}
    >
      {children}
      <div
        data-testid="message-actions"
        className={cn(
          "flex h-(--control-height-sm) items-center gap-0.5 opacity-0 transition-opacity duration-(--motion-fast)",
          "group-hover/message:opacity-100 group-focus-within/message:opacity-100",
          // Stays up for the confirmation, even if the pointer has moved on.
          copied && "opacity-100",
        )}
      >
        {time ? <span className="px-2 text-caption-11 whitespace-nowrap text-text-tertiary">{time}</span> : null}
        {meta ? (
          onDetails ? (
            <button
              type="button"
              onClick={onDetails}
              data-testid="message-open-meta"
              className="h-(--control-height-sm) cursor-pointer rounded-sm px-2 text-caption-11 whitespace-nowrap text-text-tertiary hover:text-text-primary"
            >
              {meta}
            </button>
          ) : (
            <span className="px-2 text-caption-11 whitespace-nowrap text-text-tertiary">{meta}</span>
          )
        ) : null}
        {onCopy ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={copied ? "Copied" : "Copy message"}
            data-testid="message-copy"
            onClick={() => {
              onCopy(text);
              setCopied(true);
              window.setTimeout(() => setCopied(false), 1500);
            }}
          >
            {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
          </Button>
        ) : null}
        {onEdit ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="Edit in composer"
            data-testid="message-edit"
            onClick={() => onEdit(text)}
          >
            <Pencil aria-hidden="true" />
          </Button>
        ) : null}
      </div>
    </div>
  );
}
