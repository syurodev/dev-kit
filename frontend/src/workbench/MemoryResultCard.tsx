import { useId, useLayoutEffect, useRef, useState } from "react";
import { Brain } from "lucide-react";
import { Chip } from "@/components/ui/chip";
import {
  FramedCard,
  FramedCardAction,
  FramedCardHeader,
  FramedCardMore,
  FramedCardPreview,
  FramedCardRow,
} from "@/components/ui/framed-card";
import type { BrowsePage, MemoryEntry } from "@/bridge/workspaceMemory";
import { shortAge } from "@/lib/time";
import { cn } from "@/lib/utils";

const MAX_ROWS = 10;
/** The body shows this many lines before "Show all" — line-clamp-6 below. */
const BODY_LINES = 6;
/**
 * Before the body is measured (server render, first paint): past this, a body
 * of few lines still likely wraps past six of them in a transcript column.
 */
const BODY_CHARS = 480;

/** Whether a clamped box hides text: its content runs past its own height (a pixel of rounding aside). */
export function overflowsClamp(box: { scrollHeight: number; clientHeight: number }): boolean {
  return box.scrollHeight - box.clientHeight > 1;
}

/**
 * Whether the clamped body hides anything. A guess from lines and length
 * until the box is laid out, then measured — and measured again when the
 * column changes width, since that is what rewraps it.
 */
function useClampOverflow(body: string, expanded: boolean) {
  const ref = useRef<HTMLDivElement>(null);
  const [overflowing, setOverflowing] = useState(
    () => body.split("\n").length > BODY_LINES || body.length > BODY_CHARS,
  );
  useLayoutEffect(() => {
    const el = ref.current;
    // Expanded, the box is not clamped and says nothing; keep the last answer.
    if (!el || expanded) return;
    const measure = () => setOverflowing(overflowsClamp(el));
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [body, expanded]);
  return { ref, overflowing };
}

function updated(at: string, now: number): string {
  const age = shortAge(Date.parse(at), now);
  if (!age) return "";
  return age === "now" ? "updated just now" : `updated ${age} ago`;
}

/**
 * `workspace.memory.get|create|update` — the entry as the agent read or
 * wrote it: kind, title, summary, the body folded to six lines, and which
 * revision this is. `now` is for tests; the card reads the clock otherwise.
 */
export function MemoryEntryCard({ entry, heading, now }: { entry: MemoryEntry; heading?: string; now?: number }) {
  const [expanded, setExpanded] = useState(false);
  const { ref, overflowing } = useClampOverflow(entry.body, expanded);
  const bodyId = useId();
  const when = updated(entry.updatedAt, now ?? Date.now());
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Brain}
        title={heading ?? "Workspace memory"}
        meta={[`revision ${entry.revision}`, when].filter(Boolean).join(" · ")}
        action={
          entry.body && (overflowing || expanded) ? (
            <FramedCardAction
              opens={false}
              aria-expanded={expanded}
              aria-controls={bodyId}
              onClick={() => setExpanded((value) => !value)}>
              {expanded ? "Show less" : "Show all"}
            </FramedCardAction>
          ) : undefined
        }
      />
      <FramedCardPreview>
        <div className="flex min-w-0 items-center gap-(--space-inline)">
          <Chip>{entry.kind}</Chip>
          {/* A lifecycle state, not a hazard: status colours are for how safe something is. */}
          {entry.deleted ? <span className="shrink-0 text-caption-11 text-text-tertiary">Deleted</span> : null}
          <span
            className={cn(
              "min-w-0 truncate text-body-13",
              entry.deleted ? "text-text-tertiary line-through" : "text-text-primary",
            )}
            title={entry.title}
          >
            {entry.title}
          </span>
        </div>
        <p className="text-body-13 text-text-secondary">{entry.summary}</p>
        {entry.body ? (
          <div
            ref={ref}
            id={bodyId}
            className={cn(
              "text-body-13 break-words whitespace-pre-wrap text-text-primary",
              !expanded && "line-clamp-6",
            )}
          >
            {entry.body}
          </div>
        ) : null}
      </FramedCardPreview>
    </FramedCard>
  );
}

/** `workspace.memory.list|search` — one row per entry: kind, title, summary. */
export function MemoryListCard({ page, query, heading }: { page: BrowsePage; query?: string; heading?: string }) {
  const shown = page.entries.slice(0, MAX_ROWS);
  const title = heading ?? (query ? `Workspace memory · “${query}”` : "Workspace memory");
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Brain}
        title={title}
        meta={`${page.entries.length} entr${page.entries.length === 1 ? "y" : "ies"}`}
      />
      <FramedCardPreview flush>
        {shown.length === 0 ? (
          <p className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">No entries</p>
        ) : null}
        {shown.map((entry) => (
          <FramedCardRow
            key={entry.id}
            label={
              <span title={`${entry.title} — ${entry.summary}`}>
                <span className="text-code-11 text-text-tertiary">{`[${entry.kind}]`}</span>{" "}
                <span className={cn(entry.deleted && "text-text-tertiary line-through")}>{entry.title}</span>
                <span className="text-text-tertiary">{` — ${entry.summary}`}</span>
              </span>
            }
            trailing={entry.deleted ? "Deleted" : undefined}
          />
        ))}
        <FramedCardMore count={page.entries.length - shown.length} />
        {page.hasMore ? (
          <p className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
            More results available
          </p>
        ) : null}
      </FramedCardPreview>
    </FramedCard>
  );
}
