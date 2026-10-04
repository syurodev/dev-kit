import { LoaderCircle, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import {
  CraftButton,
  CraftButtonIcon,
  CraftButtonLabel,
} from "@/components/ui/craft-button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { cn } from "@/lib/utils";

/** One thing to press. The icon sits before the label. */
export type EmptyStateAction = {
  label: string;
  icon?: LucideIcon;
  onClick: () => void;
  disabled?: boolean;
  /** Its own work is under way: the icon turns into a spinner and the button holds still. */
  busy?: boolean;
};

/**
 * The app's one "nothing here" state, in two sizes.
 *
 * `pane` — one centred stack: a 64px ring with a 26px glyph, a title, a 420px
 * body. Figma draws it as `CodeGraph / Canvas — Empty | Analyzing | Error`
 * (495:20625…) and again for Kafka's Disconnected / Connecting / Error and
 * Tools' Empty: the shape every module reaches for when a document area has
 * nothing to show yet.
 *
 * `rail` — the same idea at the size of a sidebar, a list or a side panel: no
 * ring, a 12px title over an 11px body, a small outline button. Every rail had
 * been assembling this from the `Empty` slots by hand, and they drifted — four
 * paddings, three title treatments, an `outline` button in one and a `ghost`
 * in the next. The shape lives here so there is nothing left to drift.
 *
 * Buttons are props rather than children so the rule about WHICH button is
 * written once:
 *
 *   - a lone invitation on a pane gets the craft button. It is alone, it is
 *     rare, and it is an invitation rather than an apology. Give it an icon:
 *     the button's hover wash grows out of one;
 *   - two actions share a row, so the first is a plain primary and the second
 *     an outline — a flourish beside a sibling reads as two weights for one
 *     decision;
 *   - a danger pane never gets the flourish — it would be celebrating the
 *     wrong thing;
 *   - a rail is a quiet place: outline for the first, ghost for the second.
 *
 * `children` still renders under the buttons for what is not a button: a host
 * list, a broker's error verbatim, a list of key hints.
 */
export function EmptyState({
  icon,
  busy = false,
  title,
  body,
  tone = "default",
  size = "pane",
  action,
  secondaryAction,
  className,
  children,
  ...rest
}: {
  /** Ignored while busy: the ring holds the spinner then. A rail draws it small, without a ring. */
  icon?: LucideIcon;
  /**
   * Work under way with no measure of how far along it is — the ring's glyph
   * turns. Not a bar: nothing here reports progress, and a bar that pulses at
   * 60% claims a fraction it does not have.
   */
  busy?: boolean;
  /** Optional on a rail only, where one sentence is often the whole state. */
  title?: string;
  body?: ReactNode;
  /** danger swaps the ring for the danger-soft pair — a failure, not an absence. */
  tone?: "default" | "danger";
  size?: "pane" | "rail";
  action?: EmptyStateAction;
  secondaryAction?: EmptyStateAction;
  /** Placement only (sticky, col-span, flex-none) — never padding or type. */
  className?: string;
  children?: ReactNode;
  "data-testid"?: string;
}) {
  const Icon = busy ? LoaderCircle : icon;
  const buttons = <EmptyStateButtons size={size} tone={tone} action={action} secondaryAction={secondaryAction} />;
  const hasContent = Boolean(action || secondaryAction || children);

  if (size === "rail") {
    return (
      <Empty
        data-size="rail"
        className={cn("flex-none gap-3 px-4 py-6", className)}
        role={busy ? "status" : undefined}
        aria-busy={busy || undefined}
        data-testid={rest["data-testid"]}
      >
        <EmptyHeader className="max-w-58 gap-1">
          {Icon ? (
            <EmptyMedia
              variant="icon"
              className={cn(
                "mb-1 bg-state-soft",
                tone === "danger" ? "text-status-danger" : "text-text-tertiary",
              )}
            >
              <Icon className={cn(busy && "animate-spin motion-reduce:animate-spin-slow")} aria-hidden />
            </EmptyMedia>
          ) : null}
          {title ? (
            <EmptyTitle className={cn("text-label-12", tone === "danger" ? "text-status-danger" : "text-text-secondary")}>
              {title}
            </EmptyTitle>
          ) : null}
          {body ? <EmptyDescription className="text-caption-11 text-text-tertiary">{body}</EmptyDescription> : null}
        </EmptyHeader>
        {hasContent ? (
          <EmptyContent className="max-w-58 gap-2">
            {buttons}
            {children}
          </EmptyContent>
        ) : null}
      </Empty>
    );
  }

  return (
    <Empty
      data-size="pane"
      className={cn("h-full gap-3.5", className)}
      role={busy ? "status" : undefined}
      aria-busy={busy || undefined}
      data-testid={rest["data-testid"]}
    >
      <EmptyHeader className="max-w-105 gap-3.5">
        <EmptyMedia
          className={cn(
            "mb-0 size-16 rounded-full border",
            tone === "danger"
              ? "border-(--border-danger) bg-(--feedback-danger-soft)"
              : "border-glass-border bg-state-soft",
          )}
        >
          {Icon ? (
            <Icon
              size={26}
              className={cn(
                tone === "danger" ? "text-status-danger" : "text-text-secondary",
                busy && "animate-spin motion-reduce:animate-spin-slow",
              )}
            />
          ) : null}
        </EmptyMedia>
        {title ? <EmptyTitle className="text-title-16 text-text-primary">{title}</EmptyTitle> : null}
        {body ? <EmptyDescription className="text-body-13 text-text-secondary">{body}</EmptyDescription> : null}
      </EmptyHeader>
      {hasContent ? (
        <EmptyContent className="max-w-105">
          {buttons}
          {children}
        </EmptyContent>
      ) : null}
    </Empty>
  );
}

function EmptyStateButtons({
  size,
  tone,
  action,
  secondaryAction,
}: {
  size: "pane" | "rail";
  tone: "default" | "danger";
  action?: EmptyStateAction;
  secondaryAction?: EmptyStateAction;
}) {
  if (!action && !secondaryAction) return null;

  if (size === "rail") {
    return (
      <div className="flex flex-col items-center gap-1">
        {action ? <ActionButton action={action} variant="outline" size="sm" /> : null}
        {secondaryAction ? <ActionButton action={secondaryAction} variant="ghost" size="sm" /> : null}
      </div>
    );
  }

  // The craft button's hover wash grows out of its icon, so without a glyph
  // there is nothing for it to grow from: an iconless invitation is a plain
  // primary.
  if (action?.icon && !secondaryAction && tone !== "danger") {
    const Icon = action.icon;
    return (
      <CraftButton onClick={action.onClick} disabled={action.disabled}>
        <CraftButtonIcon>
          <Icon className="size-3" />
        </CraftButtonIcon>
        <CraftButtonLabel>{action.label}</CraftButtonLabel>
      </CraftButton>
    );
  }

  return (
    <div className="flex items-center gap-2">
      {secondaryAction ? (
        <ActionButton action={secondaryAction} variant="outline" className="bg-surface-raised" />
      ) : null}
      {action ? (
        <ActionButton
          action={action}
          variant={tone === "danger" ? "outline" : "default"}
          className={tone === "danger" ? "bg-surface-raised" : undefined}
        />
      ) : null}
    </div>
  );
}

function ActionButton({
  action,
  variant,
  size,
  className,
}: {
  action: EmptyStateAction;
  variant: "default" | "outline" | "ghost";
  size?: "sm";
  className?: string;
}) {
  const Icon = action.busy ? LoaderCircle : action.icon;
  return (
    <Button
      variant={variant}
      size={size}
      className={className}
      onClick={action.onClick}
      disabled={action.disabled || action.busy}
    >
      {Icon ? <Icon className={cn(action.busy && "animate-spin")} aria-hidden /> : null}
      {action.label}
    </Button>
  );
}
