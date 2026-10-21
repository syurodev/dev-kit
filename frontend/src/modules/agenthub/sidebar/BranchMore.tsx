import { ChevronDown, MessageSquareShare } from "lucide-react";

import { cn } from "@/lib/utils";

/**
 * The last row of a branch that folds some of its children: how many are
 * folded and the way to draw them all. A button of its own (not inside a
 * card), so it nests no button. The row's own `listitem` and guide are drawn
 * by SessionBranch's BranchChildren, which knows the geometry.
 */
export function BranchMore({
  hidden,
  unread,
  open,
  controls,
  onToggle,
}: {
  /** Children folded away; the count the label reads. */
  hidden: number;
  /** Folded sessions that hold a message the person has not seen. */
  unread: number;
  /** Everything is drawn: the row is the way back. */
  open: boolean;
  /** The id of the list this row opens and closes. */
  controls: string;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      data-sidebar-nav=""
      data-testid="agent-branch-more"
      aria-expanded={open}
      aria-controls={controls}
      onClick={onToggle}
      className="flex h-7 w-full min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 text-left text-body-12 text-text-tertiary outline-none hover:text-text-secondary focus-visible:ring-2 focus-visible:ring-border-focus/50"
    >
      {open ? (
        <span className="shrink-0">Show fewer</span>
      ) : (
        <>
          <span className="shrink-0 tabular-nums">{`+${hidden} more`}</span>
          <span aria-hidden="true" className="shrink-0">
            ·
          </span>
          <span className="shrink-0">Show all</span>
        </>
      )}
      {!open && unread > 0 ? (
        <span
          data-testid="agent-branch-more-unread"
          role="img"
          aria-label={unread === 1 ? "1 folded session has new messages" : `${unread} folded sessions have new messages`}
          className="flex shrink-0 items-center gap-0.5 text-caption-11 text-text-secondary tabular-nums"
        >
          <MessageSquareShare aria-hidden="true" className="size-3" />
          {unread > 9 ? "9+" : unread}
        </span>
      ) : null}
      <ChevronDown
        aria-hidden="true"
        className={cn(
          "ml-auto size-3.5 shrink-0 transition-transform duration-(--motion-fast) ease-(--ease-out-strong) motion-reduce:transition-none",
          open && "rotate-180",
        )}
      />
    </button>
  );
}
