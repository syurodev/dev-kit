import type { Review } from "@/bridge/harness";

/**
 * The host-written account of a prepared tool-group change: the saved
 * configuration and the running session are listed apart, never merged. Nothing
 * here comes from the agent; an agent's own words are shown by the confirmation,
 * labelled as untrusted.
 */
export function HarnessReviewBody({ review }: { review: Review }) {
  return (
    <section aria-label="Review changes" className="min-h-0 space-y-3">
      <p className="whitespace-pre-wrap break-words text-body-13 text-text-primary">{review.summary}</p>
      <ul className="space-y-1.5">
        {(review.changes ?? []).map((change, index) => (
          <li
            key={`${change.scope}-${change.field}-${change.key}-${index}`}
            className="flex items-start gap-2 rounded-md border border-border-subtle px-3 py-2 text-body-12"
          >
            <span className="shrink-0 rounded bg-state-soft px-1.5 py-0.5 text-caption-11 text-text-secondary">
              {change.scope === "running" ? "Running session" : "Saved configuration"}
            </span>
            <span className="min-w-0 break-words text-text-primary">{change.effect}</span>
          </li>
        ))}
      </ul>
      <p className="text-caption-11 text-text-tertiary">
        {review.needsConfirmation
          ? "DevKit will ask you to confirm before anything is saved."
          : "Nothing else changes."}
      </p>
    </section>
  );
}
