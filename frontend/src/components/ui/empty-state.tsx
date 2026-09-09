import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * One centred stack — a 64px ring with a 26px glyph, a title, a 420px body —
 * and a slot under it for whatever the state adds (a ⌘K chip, a progress
 * bar, two buttons). Figma draws it as `CodeGraph / Canvas — Empty |
 * Analyzing | Error` (495:20625…) and again for Kafka's Disconnected /
 * Connecting / Error and Tools' Empty: the same shape every module reaches
 * for when a pane has nothing to show yet, so it lives here.
 */
export function EmptyState({
  icon: Icon,
  title,
  body,
  tone = "default",
  children,
}: {
  icon: LucideIcon;
  title: string;
  body: string;
  /** danger swaps the ring for the danger-soft pair — a failure, not an absence. */
  tone?: "default" | "danger";
  children?: ReactNode;
}) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3.5 p-6 text-center">
      <span
        className={cn(
          "flex size-16 items-center justify-center rounded-full border",
          tone === "danger"
            ? "border-(--border-danger) bg-(--feedback-danger-soft)"
            : "border-glass-border bg-state-soft",
        )}
      >
        <Icon size={26} className={tone === "danger" ? "text-status-danger" : "text-text-secondary"} />
      </span>
      <h3 className="text-title-16 text-text-primary">{title}</h3>
      <p className="max-w-105 text-body-13 text-text-secondary">{body}</p>
      {children}
    </div>
  );
}

/** The `progress` bar (495:20645): 280 × 4, status/info over state-soft. */
export function EmptyProgress({ fraction }: { fraction?: number }) {
  return (
    <span className="block h-1 w-70 overflow-hidden rounded-full bg-state-soft">
      <span
        className={fraction == null ? "block h-full w-3/5 animate-pulse rounded-full bg-status-info" : "block h-full rounded-full bg-status-info"}
        style={fraction == null ? undefined : { width: `${Math.round(fraction * 100)}%` }}
      />
    </span>
  );
}
