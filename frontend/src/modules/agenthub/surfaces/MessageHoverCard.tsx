import * as React from "react";
import { Check, Copy, Pencil } from "lucide-react";

import { cn } from "@/lib/utils";
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
 * The keyboard gets the same actions as an inline row, not the card. The card
 * lives in a portal, so Tab from the focused message went on to the next
 * message, the trigger blurred and the card closed before Copy or Edit could
 * be reached. The row sits in the page right after the message and shows only
 * while focus-visible is inside this message — so Tab lands on it next, a
 * pointer hovering or clicking moves no layout, and the card does not also
 * open on that focus.
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
  const child = children as React.ReactElement<React.HTMLAttributes<HTMLElement>>;
  const trigger = React.cloneElement(child, {
    tabIndex: child.props.tabIndex ?? 0,
    "aria-label": child.props["aria-label"] ?? `Message: ${text.slice(0, 120)}`,
  });

  const actions = (
    <>
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
    </>
  );

  return (
    <div className={cn("group/message flex w-full flex-col gap-1", align === "end" ? "items-end" : "items-start")}>
      <HoverCard openDelay={300} closeDelay={120}>
        {/* Focus is the inline row's job; preventDefault stops Radix opening the card too. */}
        <HoverCardTrigger asChild onFocus={(e) => e.preventDefault()}>
          {trigger}
        </HoverCardTrigger>
        <HoverCardContent
          side="bottom"
          align={align}
          sideOffset={4}
          data-testid="message-hover-card"
          className="flex w-auto items-center gap-1 p-1"
        >
          {actions}
        </HoverCardContent>
      </HoverCard>
      <div data-testid="message-actions" className="hidden items-center gap-1 group-has-[:focus-visible]/message:flex">
        {actions}
      </div>
    </div>
  );
}
