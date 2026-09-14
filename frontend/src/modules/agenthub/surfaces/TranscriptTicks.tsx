import { useState } from "react";
import { cn } from "@/lib/utils";

export type TranscriptTick = {
  id: string;
  prompt: string;
  /** The start of the reply, as a second line in the preview. */
  answer?: string;
};

export type TranscriptTicksProps = {
  ticks: TranscriptTick[];
  /** The tick whose prompt is at the top of the view. */
  active: number;
  onJump: (id: string) => void;
  className?: string;
};

/**
 * One tick per prompt you sent, down the left edge of the transcript: a map of
 * the conversation you can jump along. The tick for the prompt you are reading
 * is lit. Under the pointer the ticks swell like a dock — the one under it
 * most, its neighbours a little — so a 2px target reads as where you are, and
 * the prompt with the start of its answer shows beside it.
 */
export function TranscriptTicks({ ticks, active, onJump, className }: TranscriptTicksProps) {
  const [hovered, setHovered] = useState<number | null>(null);
  if (ticks.length < 2) return null;
  const preview = hovered === null ? null : ticks[hovered];

  return (
    <nav
      aria-label="Your messages"
      data-testid="transcript-ticks"
      className={cn("flex flex-col items-start", className)}
      onMouseLeave={() => setHovered(null)}
    >
      {ticks.map((tick, index) => {
        const distance = hovered === null ? Infinity : Math.abs(index - hovered);
        return (
          <button
            key={tick.id}
            type="button"
            data-testid="transcript-tick"
            data-active={index === active || undefined}
            aria-label={tick.prompt}
            aria-current={index === active ? "location" : undefined}
            onClick={() => onJump(tick.id)}
            onMouseEnter={() => setHovered(index)}
            onFocus={() => setHovered(index)}
            onBlur={() => setHovered(null)}
            // The button is the full row height, so the gaps between ticks
            // are part of a target rather than dead space.
            className="group flex h-2.5 w-8 cursor-pointer items-center focus-visible:outline-none"
          >
            <span
              className={cn(
                "block h-0.5 rounded-full transition-all duration-150 ease-out motion-reduce:transition-none",
                distance === 0 ? "w-7" : distance === 1 ? "w-5" : distance === 2 ? "w-4" : "w-3",
                index === active || distance === 0
                  ? "bg-text-primary"
                  : "bg-text-tertiary group-focus-visible:bg-text-secondary",
              )}
            />
          </button>
        );
      })}

      {preview ? (
        <div
          role="presentation"
          data-testid="transcript-tick-preview"
          className="pointer-events-none absolute left-full ml-2 flex w-80 flex-col gap-1 rounded-xl p-3 glass-float"
          // Beside the tick it describes: each row is h-2.5, 10px.
          style={{ top: `calc(${hovered} * var(--spacing) * 2.5 - var(--spacing) * 3)` }}
        >
          <p className="truncate text-body-14 text-text-primary">{preview.prompt}</p>
          {preview.answer ? (
            <p className="line-clamp-2 text-body-13 text-text-tertiary">{preview.answer}</p>
          ) : null}
        </div>
      ) : null}
    </nav>
  );
}

/** The ticks for a list of turns: those that start with something you typed. */
export function ticksOf(
  turns: { id: string; prompt?: { text?: string }; answer: { text?: string }[] }[],
): TranscriptTick[] {
  return turns.flatMap((turn) => {
    const prompt = turn.prompt?.text?.trim();
    if (!prompt) return [];
    const answer = turn.answer.map((item) => item.text ?? "").join("\n");
    // Markdown marks read as noise in a two-line preview. Hyphens inside
    // words stay: only a list bullet's dash goes.
    const plain = answer
      .replace(/^```.*$/gm, "")
      .replace(/^\s*\|?[\s:|-]*-[\s:|-]*\|?\s*$/gm, "")
      .replace(/^\s*[-+]\s+/gm, "")
      .replace(/[#*`|>]+/g, " ")
      .replace(/\s+/g, " ")
      .trim();
    return [{ id: turn.id, prompt: prompt.replace(/\s+/g, " "), answer: plain || undefined }];
  });
}
