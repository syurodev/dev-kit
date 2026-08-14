import { cn } from "@/lib/utils";
import type { NoteSummary } from "../../bridge/notes";
import { notePanelStyle } from "./noteColor";

function relativeTime(iso: string): string {
  const diffMs = Date.now() - new Date(iso).getTime();
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;
  if (diffMs < hour) return `${Math.max(1, Math.round(diffMs / minute))}m`;
  if (diffMs < day) return `${Math.round(diffMs / hour)}h`;
  return `${Math.round(diffMs / day)}d`;
}

export function NoteCard({
  note,
  onOpen,
  onTagClick,
}: {
  note: NoteSummary;
  onOpen: (id: string) => void;
  onTagClick: (tag: string) => void;
}) {
  const hasColor = note.effectiveColor !== "";
  return (
    <button
      type="button"
      onClick={() => onOpen(note.id)}
      className="flex aspect-[3/2] flex-col rounded-2xl border border-border bg-card p-[5px] text-left"
    >
      <span className="truncate px-1.5 pb-1.5 pt-1 text-[13px] font-semibold text-foreground">
        {note.title || "Untitled"}
      </span>
      <span
        className={cn(
          "flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border p-2.5",
          hasColor ? "" : "border-transparent bg-muted",
        )}
        style={hasColor ? notePanelStyle(note.effectiveColor) : undefined}
      >
        <span className="line-clamp-3 flex-1 text-[10px] leading-relaxed text-foreground/90">
          {note.preview || "No preview"}
        </span>
        <span className="mt-1.5 flex items-center justify-between gap-2">
          <span className="flex flex-wrap gap-2">
            {note.effectiveTags.map((tag) => (
              <span
                key={tag.toLowerCase()}
                role="button"
                tabIndex={0}
                className="flex items-center gap-1 text-[9px] font-medium text-foreground/80"
                onClick={(e) => {
                  e.stopPropagation();
                  onTagClick(tag);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    e.stopPropagation();
                    onTagClick(tag);
                  }
                }}
              >
                <span className="size-[5px] shrink-0 rounded-full bg-current" aria-hidden />
                {tag}
              </span>
            ))}
          </span>
          <span className="shrink-0 text-[8.5px] text-foreground/60">
            {relativeTime(note.updatedAt)}
          </span>
        </span>
      </span>
    </button>
  );
}
