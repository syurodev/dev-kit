import * as React from "react";
import { ArrowUpRight } from "lucide-react";

import { cn } from "@/lib/utils";

/**
 * Card with header and preview — docs/dev-kit/framed-card.mdx is the contract;
 * this is its one implementation, for every module.
 *
 * The header's surface wraps the preview: the card is one raised surface, the
 * header sits on it, and the preview is an inset panel on the page's own
 * surface, framed by the raised one on three sides at --space-micro (4px). The
 * frame is what makes the header read as the preview's title rather than as a
 * separate row above it.
 *
 * Radii are nested, not picked: the preview is --radius-control (10) and the
 * card --radius-float (14) = 10 + the 4px frame, so the two curves run
 * parallel. Both are on the design system's radius scale (control 6–10,
 * container 14–18).
 *
 * Content lines up on one edge: the header's padding (--space-inline, 8) plus
 * the frame is 12 from the card's edge, and so is the preview's (the frame
 * plus its own --space-inline).
 */
export function FramedCard({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="framed-card"
      className={cn(
        "flex w-full min-w-0 flex-col rounded-float bg-surface-raised p-(--space-micro) text-text-primary",
        className,
      )}
      {...props}
    />
  );
}

export type FramedCardHeaderProps = {
  icon?: React.ComponentType<{ className?: string }>;
  /** What the card is about — a noun, not a sentence: "API flow", "Commit". */
  title: React.ReactNode;
  /** One short fact beside the title that never wraps: a count, a status, a SHA. */
  meta?: React.ReactNode;
  /** The card's one action, at the far end (FramedCardAction). */
  action?: React.ReactNode;
  className?: string;
};

/**
 * The header row: icon, title, meta, and one action at the end. Control-height
 * sm (28), so a card's header lines up with the toolbars around it.
 */
export function FramedCardHeader({ icon: Icon, title, meta, action, className }: FramedCardHeaderProps) {
  return (
    <div
      data-slot="framed-card-header"
      className={cn("flex h-(--control-height-sm) min-w-0 items-center gap-(--space-inline) px-(--space-inline)", className)}
    >
      {Icon ? <Icon className="size-(--icon-12) shrink-0 text-icon-secondary" aria-hidden="true" /> : null}
      <span className="min-w-0 truncate text-label-12 text-text-secondary">{title}</span>
      {meta !== undefined ? (
        <span data-slot="framed-card-meta" className="shrink-0 text-caption-11 text-text-tertiary tabular-nums">
          {meta}
        </span>
      ) : null}
      {action ? <span className="ml-auto flex shrink-0 items-center">{action}</span> : null}
    </div>
  );
}

/**
 * The header's action: a quiet text button. Quiet because the preview is the
 * card's content; the action is the way out of it — or, for a preview whose
 * content is the point (code), a way to take it: Copy. `opens` (the default)
 * adds the arrow that says it opens something elsewhere; a copy has none,
 * and carries its own glyph in `children`.
 */
export function FramedCardAction({
  children,
  opens = true,
  className,
  ...props
}: Omit<React.ComponentProps<"button">, "type"> & { opens?: boolean }) {
  return (
    <button
      type="button"
      data-slot="framed-card-action"
      className={cn(
        "-mr-(--space-micro) inline-flex h-(--control-height-xs) cursor-pointer items-center gap-(--space-micro) rounded-sm px-(--space-inline) text-caption-11 text-text-tertiary [&_svg]:size-(--icon-12) [&_svg]:shrink-0",
        "transition-colors duration-(--motion-fast) hover:bg-(--state-soft) hover:text-text-primary",
        "focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none",
        className,
      )}
      {...props}
    >
      {children}
      {opens ? <ArrowUpRight aria-hidden="true" /> : null}
    </button>
  );
}

/**
 * The preview: an inset panel on the page's surface, framed by the card.
 * `flush` drops its padding for content that brings its own — a list of
 * FramedCardRows, whose hover fills the preview edge to edge.
 */
export function FramedCardPreview({
  flush = false,
  className,
  ...props
}: React.ComponentProps<"div"> & { flush?: boolean }) {
  return (
    <div
      data-slot="framed-card-preview"
      className={cn(
        "flex min-w-0 flex-col overflow-hidden rounded-control bg-surface-base",
        flush ? "py-(--space-micro)" : "gap-(--space-micro) p-(--space-inline)",
        className,
      )}
      {...props}
    />
  );
}

export type FramedCardRowProps = {
  icon?: React.ComponentType<{ className?: string }>;
  /** Line 1 — the thing itself. Truncates before `trailing` gives an inch. */
  label: React.ReactNode;
  /** Line 1, right — short, never wraps. */
  trailing?: React.ReactNode;
  /** Line 2 — muted detail, already joined (e.g. by " · "). */
  detail?: React.ReactNode;
  onClick?: () => void;
  className?: string;
};

/**
 * One item of a list inside a flush preview. A click target fills the row
 * edge to edge on hover — the preview's rounding clips it — so the list reads
 * as the preview's own content, not as buttons laid on it.
 */
export function FramedCardRow({ icon: Icon, label, trailing, detail, onClick, className }: FramedCardRowProps) {
  const Tag = onClick ? "button" : "div";
  return (
    <Tag
      type={onClick ? "button" : undefined}
      onClick={onClick}
      data-slot="framed-card-row"
      className={cn(
        "flex w-full min-w-0 flex-col gap-0.5 px-(--space-inline) py-(--space-micro) text-left transition-colors duration-(--motion-fast)",
        onClick && "cursor-pointer hover:bg-(--state-soft) focus-visible:bg-(--state-soft) focus-visible:outline-none",
        className,
      )}
    >
      <span className="flex w-full min-w-0 items-center gap-(--space-inline)">
        {Icon ? <Icon className="size-(--icon-12) shrink-0 text-icon-secondary" aria-hidden="true" /> : null}
        <span className="min-w-0 flex-1 truncate text-body-13 text-text-primary">{label}</span>
        {trailing !== undefined ? (
          <span className="shrink-0 text-caption-11 text-text-tertiary tabular-nums">{trailing}</span>
        ) : null}
      </span>
      {detail ? (
        // 5 = 20px: the 12px icon plus the --space-inline gap beside it — an
        // indent lined up with the label above, not a spacing step.
        <span className={cn("block w-full min-w-0 truncate text-caption-11 text-text-tertiary", Icon && "pl-5")}>
          {detail}
        </span>
      ) : null}
    </Tag>
  );
}

/** The last line of a capped list: how many more there are, with nothing to click. */
export function FramedCardMore({ count }: { count: number }) {
  if (count <= 0) return null;
  return (
    <div data-slot="framed-card-more" className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
      +{count} more
    </div>
  );
}
