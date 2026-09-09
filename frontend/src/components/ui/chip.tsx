import { cn } from "@/lib/utils";

/**
 * Figma `chip/*` (Database 742:10, Kafka 1131:2): 24 tall, radius 8, Code/11 —
 * surface/raised on a subtle stroke by default, the danger-soft pair for
 * anything that should give pause (prod). One fact about a thing — the
 * engine, the environment, a partition count — set beside its name.
 */
export function Chip({
  tone = "default",
  className,
  children,
}: {
  tone?: "default" | "danger";
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <span
      className={cn(
        "inline-flex h-6 shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-code-11 whitespace-nowrap",
        tone === "danger"
          ? "border-(--border-danger) bg-(--feedback-danger-soft) text-status-danger"
          : "border-border-subtle bg-surface-raised text-text-secondary",
        className,
      )}
    >
      {children}
    </span>
  );
}
