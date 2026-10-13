import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/lib/utils";
import type { MarkKind } from "./rail";

/**
 * The small pieces the Git module is drawn from — Figma page `21 Git`, Parts.
 * They are here rather than in components/ui because nothing outside Git has a
 * use for a status letter or a stage button; the diff itself is the shared
 * Diff / Line (components/ui/diff).
 */

const MARK: Record<MarkKind, { letter: string; className: string }> = {
  modified: {
    letter: "M",
    className: "bg-status-warning text-action-primary-foreground",
  },
  added: {
    letter: "A",
    className: "bg-status-success text-action-primary-foreground",
  },
  deleted: {
    letter: "D",
    className: "bg-status-danger text-action-primary-foreground",
  },
  untracked: {
    letter: "U",
    className: "bg-(--hue-blue) text-action-primary-foreground",
  },
  renamed: {
    letter: "R",
    className: "bg-status-recovery text-action-primary-foreground",
  },
  // Outlined rather than filled: a conflict is the one mark that asks for
  // something, and a solid red block down a rail reads as an error.
  conflict: {
    letter: "!",
    className: "border border-(--border-danger) bg-(--feedback-danger-soft) text-status-danger",
  },
};

const MARK_NAME: Record<MarkKind, string> = {
  modified: "Modified",
  added: "Added",
  deleted: "Deleted",
  untracked: "Untracked",
  renamed: "Renamed",
  conflict: "Conflicted",
};

/** Git / Status Mark: one letter for what happened to a file. */
export function StatusMark({ kind }: { kind: MarkKind }) {
  const { letter, className } = MARK[kind];
  return (
    <span
      role="img"
      aria-label={MARK_NAME[kind]}
      className={cn(
        "inline-flex h-4.5 w-5 shrink-0 items-center justify-center rounded-md text-code-11 leading-none font-semibold",
        className,
      )}
    >
      {letter}
    </span>
  );
}

/**
 * An action drawn as its icon alone, named by a tooltip that is also its
 * accessible name. Every Git control that used to be a word beside a row or a
 * heading is this: the list is the content, and the buttons only need to be
 * findable. `wrap` is always on because most of them are disabled while a Git
 * operation runs, and a disabled button gives a tooltip nothing to hang on.
 */
export function IconAction({
  label,
  icon,
  size = "icon-sm",
  variant = "ghost",
  side = "bottom",
  className,
  ...props
}: Omit<ComponentProps<"button">, "children" | "aria-label"> & {
  label: string;
  icon: ReactNode;
  size?: "icon-xs" | "icon-sm";
  variant?: "ghost" | "outline" | "default";
  side?: "top" | "right" | "bottom" | "left";
}) {
  return (
    <ActionTooltip label={label} side={side} wrap>
      <Button type="button" variant={variant} size={size} className={cn("shrink-0", className)} {...props}>
        {icon}
      </Button>
    </ActionTooltip>
  );
}

/** The small action at the end of a row: Stage, Unstage, Stage hunk. */
export function RowAction({ label, icon, ...props }: Omit<ComponentProps<typeof IconAction>, "variant" | "size">) {
  return <IconAction variant="outline" label={label} icon={icon} {...props} />;
}

/** A quiet action in a section heading: Stage all, Unstage all, Refresh. */
export function HeadingAction({
  label,
  icon,
  className,
  ...props
}: Omit<ComponentProps<typeof IconAction>, "variant" | "size">) {
  return <IconAction size="icon-xs" label={label} icon={icon} className={cn("text-text-tertiary", className)} {...props} />;
}

/** A fact about the folder or branch, in a soft pill — with a dot when it has a state to show. */
export function StatePill({
  dot,
  icon,
  className,
  children,
  title,
}: {
  dot?: "success" | "info" | "warning";
  icon?: ReactNode;
  className?: string;
  children: ReactNode;
  title?: string;
}) {
  return (
    <span
      title={title}
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 rounded-md bg-state-soft px-2 py-0.75 text-caption-11 whitespace-nowrap text-text-secondary",
        className,
      )}
    >
      {dot ? (
        <span
          aria-hidden
          className={cn(
            "size-1.5 shrink-0 rounded-full",
            dot === "success" && "bg-status-success",
            dot === "info" && "bg-status-info",
            dot === "warning" && "bg-status-warning",
          )}
        />
      ) : null}
      {icon}
      {children}
    </span>
  );
}
