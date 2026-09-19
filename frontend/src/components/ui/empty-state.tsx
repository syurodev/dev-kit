import { LoaderCircle, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { cn } from "@/lib/utils";

/**
 * One centred stack — a 64px ring with a 26px glyph, a title, a 420px body —
 * and a slot under it for whatever the state adds (a ⌘K chip, two buttons). Figma draws it as `CodeGraph / Canvas — Empty |
 * Analyzing | Error` (495:20625…) and again for Kafka's Disconnected /
 * Connecting / Error and Tools' Empty: the same shape every module reaches
 * for when a pane has nothing to show yet, so it lives here.
 *
 * The stack itself is the shared `Empty` primitive. This wrapper is the
 * module-facing shape (icon + title + body + tone) drawn on top of it, so
 * there is one set of empty-state slots in the app rather than two that drift.
 */
export function EmptyState({
  icon,
  busy = false,
  title,
  body,
  tone = "default",
  children,
}: {
  /** Ignored while busy: the ring holds the spinner then. */
  icon?: LucideIcon;
  /**
   * Work under way with no measure of how far along it is — the ring's glyph
   * turns. Not a bar: nothing here reports progress, and a bar that pulses at
   * 60% claims a fraction it does not have.
   */
  busy?: boolean;
  title: string;
  body: string;
  /** danger swaps the ring for the danger-soft pair — a failure, not an absence. */
  tone?: "default" | "danger";
  children?: ReactNode;
}) {
  const Icon = busy ? LoaderCircle : icon;
  return (
    <Empty className="h-full gap-3.5" role={busy ? "status" : undefined} aria-busy={busy || undefined}>
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
                busy && "animate-spin motion-reduce:animate-[spin_3s_linear_infinite]",
              )}
            />
          ) : null}
        </EmptyMedia>
        <EmptyTitle className="text-title-16 text-text-primary">{title}</EmptyTitle>
        <EmptyDescription className="text-body-13 text-text-secondary">{body}</EmptyDescription>
      </EmptyHeader>
      {children ? <EmptyContent className="max-w-105">{children}</EmptyContent> : null}
    </Empty>
  );
}
