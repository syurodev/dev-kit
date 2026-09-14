import * as React from "react";
import { Check, Copy, Pencil } from "lucide-react";

import { Button } from "@/components/ui/button";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";

export type MessageHoverCardProps = {
  /** The message itself: a single DOM element, which becomes the trigger. */
  children: React.ReactElement;
  /** Which edge of the message the card lines up with — yours on the right, the agent's on the left. */
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
 * A message's time and its quick actions, on hover rather than under it.
 *
 * Under every message they were a line of chrome repeated down the whole
 * transcript — a timestamp and two icons for each of fifty messages, read for
 * one of them. On hover they are there for the message you are pointing at and
 * nowhere else, and the transcript is just what was said.
 *
 * A hover card, not a tooltip: it holds controls, and a pointer can travel
 * into it. The short open delay keeps a pointer passing down the column from
 * lighting a card on every message it crosses.
 *
 * Hover is the only way in. Copying stays possible without it — the text is
 * selectable — and Edit is a convenience for retyping, so nothing becomes
 * unreachable from the keyboard; it is just slower.
 */
export function MessageHoverCard({
  children,
  align,
  text,
  at,
  meta,
  onDetails,
  onCopy,
  onEdit,
}: MessageHoverCardProps) {
  const [copied, setCopied] = React.useState(false);
  const time = timeOf(at);
  if (!time && !meta && !onCopy && !onEdit) return children;

  return (
    <HoverCard openDelay={300} closeDelay={120}>
      <HoverCardTrigger asChild>{children}</HoverCardTrigger>
      <HoverCardContent
        side="bottom"
        align={align}
        sideOffset={4}
        data-testid="message-hover-card"
        className="flex w-auto items-center gap-1 p-1"
      >
        {time ? <span className="px-2 text-caption-11 whitespace-nowrap text-text-tertiary">{time}</span> : null}
        {meta ? (
          onDetails ? (
            <button
              type="button"
              onClick={onDetails}
              data-testid="message-open-meta"
              className="cursor-pointer rounded-sm px-2 text-caption-11 whitespace-nowrap text-text-tertiary hover:text-text-primary"
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
            size="icon-xs"
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
            size="icon-xs"
            aria-label="Edit in composer"
            data-testid="message-edit"
            onClick={() => onEdit(text)}
          >
            <Pencil aria-hidden="true" />
          </Button>
        ) : null}
      </HoverCardContent>
    </HoverCard>
  );
}
