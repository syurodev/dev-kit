import { Tag } from "lucide-react";
import type { NoteSummary } from "../../bridge/notes";
import { noteAccentColor } from "./noteColor";
import { tagSoftBackground } from "./tagColor";

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
  onOpenDouble,
  onPreload,
  onTagClick,
}: {
  note: NoteSummary;
  onOpen: (note: NoteSummary) => void;
  onOpenDouble: (note: NoteSummary) => void;
  onPreload?: () => void;
  onTagClick: (tag: string) => void;
}) {
  const accent = noteAccentColor(note.effectiveColor);
  return (
    <button
      type="button"
      onMouseEnter={onPreload}
      onFocus={onPreload}
      onClick={() => onOpen(note)}
      onDoubleClick={() => onOpenDouble(note)}
      className="flex aspect-3/2 flex-col gap-1.5 rounded-xl border border-border bg-card p-4 text-left [contain-intrinsic-size:auto_208px] [content-visibility:auto]"
    >
      <span className="flex min-w-0 items-center gap-1.5">
        {accent ? (
          <span
            className="size-2 shrink-0 rounded-[4px]"
            style={{ background: accent }}
            aria-hidden
          />
        ) : null}
        <span className="min-w-0 truncate text-base font-semibold tracking-[0.2px] text-foreground">
          {note.title || "Untitled"}
        </span>
      </span>
      <span className="line-clamp-3 flex-1 text-[13px] leading-[1.4] tracking-[0.1px] text-muted-foreground">
        {note.preview || "No preview"}
      </span>
      <span className="flex items-center justify-between gap-2">
        <span className="flex flex-wrap gap-1">
          {note.effectiveTags.map((tag) => (
            <span
              key={tag.toLowerCase()}
              role="button"
              tabIndex={0}
              className="inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-semibold tracking-[0.4px]"
              style={tagSoftBackground(tag)}
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
              <Tag className="size-2.5" />
              {tag}
            </span>
          ))}
        </span>
        <span className="shrink-0 text-xs tracking-[0.4px] text-muted-foreground">
          {relativeTime(note.updatedAt)}
        </span>
      </span>
    </button>
  );
}
