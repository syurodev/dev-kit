import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import type { MarkKind } from "./rail";

/**
 * The small pieces the Git module is drawn from — Figma page `21 Git`, Parts.
 * They are here rather than in components/ui because nothing outside Git has a
 * use for a status letter or a stage button; the diff itself is the shared
 * Diff / Line (components/ui/diff).
 */

const MARK: Record<MarkKind, { letter: string; className: string }> = {
  modified: { letter: "M", className: "bg-status-warning text-action-primary-foreground" },
  added: { letter: "A", className: "bg-status-success text-action-primary-foreground" },
  deleted: { letter: "D", className: "bg-status-danger text-action-primary-foreground" },
  untracked: { letter: "U", className: "bg-status-info text-action-primary-foreground" },
  renamed: { letter: "R", className: "bg-status-recovery text-action-primary-foreground" },
  // Outlined rather than filled: a conflict is the one mark that asks for
  // something, and a solid red block down a rail reads as an error.
  conflict: { letter: "!", className: "border border-(--border-danger) bg-(--feedback-danger-soft) text-status-danger" },
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

/** The small bordered action at the end of a row: Stage, Unstage, Stage hunk. */
export function RowAction({
  children,
  className,
  ...props
}: React.ComponentProps<"button">) {
  return (
    <button
      type="button"
      className={cn(
        "shrink-0 rounded-md border border-border-subtle px-2.5 py-0.75 text-label-12 text-text-secondary transition-colors duration-(--motion-fast) ease-(--ease-out-strong) hover:bg-state-soft hover:text-text-primary disabled:pointer-events-none disabled:opacity-38",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}

/** A quiet text action in a section heading: Stage all, Unstage all, Refresh. */
export function HeadingAction({ children, className, ...props }: React.ComponentProps<"button">) {
  return (
    <button
      type="button"
      className={cn(
        "shrink-0 text-caption-11 text-text-tertiary transition-colors duration-(--motion-fast) ease-(--ease-out-strong) hover:text-text-primary disabled:pointer-events-none disabled:opacity-38",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
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
