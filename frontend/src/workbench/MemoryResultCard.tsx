import { useState } from "react";
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
import { shortAge } from "@/modules/agenthub/sidebar/model";
import { cn } from "@/lib/utils";

const MAX_ROWS = 10;
/** The body shows this many lines before "Show all" — line-clamp-6 below. */
const BODY_LINES = 6;
/** Past this, a body of few lines still wraps past six of them in a transcript column. */
const BODY_CHARS = 480;

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
  const long = entry.body.split("\n").length > BODY_LINES || entry.body.length > BODY_CHARS;
  const when = updated(entry.updatedAt, now ?? Date.now());
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Brain}
        title={heading ?? "Workspace memory"}
        meta={[`revision ${entry.revision}`, when].filter(Boolean).join(" · ")}
        action={
          long ? (
            <FramedCardAction opens={false} aria-expanded={expanded} onClick={() => setExpanded((value) => !value)}>
              {expanded ? "Show less" : "Show all"}
            </FramedCardAction>
          ) : undefined
        }
      />
      <FramedCardPreview>
        <div className="flex min-w-0 items-center gap-(--space-inline)">
          <Chip>{entry.kind}</Chip>
          {entry.deleted ? <Chip tone="danger">Deleted</Chip> : null}
          <span className="min-w-0 truncate text-body-13 text-text-primary" title={entry.title}>
            {entry.title}
          </span>
        </div>
        <p className="text-body-13 text-text-secondary">{entry.summary}</p>
        {entry.body ? (
          <div
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
                {entry.title}
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
