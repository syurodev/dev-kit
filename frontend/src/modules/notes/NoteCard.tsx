import { MoreHorizontal } from "lucide-react";
import type { NoteSummary } from "../../bridge/notes";
import { noteCardSurface } from "./noteColor";
import { relativeTime } from "./relativeTime";
import { tagChipVar } from "./tagColor";

/**
 * Figma `Note Card`, Mode=Normal (798:3178): 348×148, radius/10, p-16,
 * folder-tinted surface and stroke, title row with an overflow glyph, a
 * two-line body, and a footer of tag dot + name on the left and the age on
 * the right — both in Code/11. No accent dot beside the title: the whole
 * card is the colour.
 */
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
  return (
    <button
      type="button"
      onMouseEnter={onPreload}
      onFocus={onPreload}
      onClick={() => onOpen(note)}
      onDoubleClick={() => onOpenDouble(note)}
      style={noteCardSurface(note.effectiveColor)}
      className="flex h-37 flex-col gap-2.5 rounded-control border p-4 text-left [contain-intrinsic-size:auto_148px] [content-visibility:auto]"
    >
      <span className="flex min-w-0 items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-title-16 text-text-primary">
          {note.title || "Untitled"}
        </span>
        {/* The card's overflow affordance. Nothing hangs off it yet — the
            actions a note has (trash, move) live in the editor — so it is
            drawn, not wired, until there is a menu worth opening. */}
        <MoreHorizontal className="size-3.5 shrink-0 text-text-tertiary" aria-hidden />
      </span>
      <span className="line-clamp-2 flex-1 text-body-13 text-text-secondary">
        {note.preview || "No preview"}
      </span>
      <span className="flex items-center gap-2">
        <span className="flex min-w-0 flex-1 items-center gap-2 overflow-hidden">
          {note.effectiveTags.map((tag) => (
            <span
              key={tag.toLowerCase()}
              role="button"
              tabIndex={0}
              className="flex shrink-0 items-center gap-1.5 text-code-11 text-text-secondary hover:text-text-primary"
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
              <span
                className="size-1.5 rounded-full"
                style={{ background: tagChipVar(tag) }}
                aria-hidden
              />
              {tag}
            </span>
          ))}
        </span>
        <span className="shrink-0 text-code-11 text-text-secondary">
          {relativeTime(note.updatedAt)} ago
        </span>
      </span>
    </button>
  );
}
