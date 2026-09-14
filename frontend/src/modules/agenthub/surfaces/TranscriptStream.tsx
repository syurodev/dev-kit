import { type CSSProperties, useEffect, useMemo, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import type { TranscriptItem } from "../types";
import { groupTurns } from "../turns";
import { AgentJumpToLatest } from "./AgentJumpToLatest";
import { AgentMetaInspector } from "./AgentMetaInspector";
import { AgentTurn } from "./AgentTurn";
import { TranscriptTicks, ticksOf } from "./TranscriptTicks";

export type TranscriptStreamProps = {
  items: TranscriptItem[];
  /** The vendor, for marks and metadata — not what a message is signed with. */
  vendor?: string;
  /** A turn is in flight. The newest turn runs its orb and follows its steps. */
  streaming?: boolean;
  /** Copy, in each message's hover card. */
  onCopy?: (text: string) => void;
  /** Edit, in your own prompts' hover cards: the text goes back in the composer. */
  onEditPrompt?: (text: string) => void;
  loading?: boolean;
  className?: string;
};

/**
 * TranscriptStream
 *
 * The conversation as turns — question, folded work, answer — in one reading
 * column. Owns the scroll position, the floating AgentJumpToLatest button and
 * the meta inspector.
 *
 * The column is max-w-180, the 720 the notes editor writes in. At the full
 * width of a wide window an answer ran to 200 characters a line, and a bubble
 * pinned to the far right edge sat a whole window away from the reply under it.
 */
export function TranscriptStream({
  items,
  vendor = "Claude",
  streaming = false,
  onCopy,
  onEditPrompt,
  loading = false,
  className,
}: TranscriptStreamProps) {
  const turns = useMemo(() => groupTurns(items), [items]);
  const ticks = useMemo(() => ticksOf(turns), [turns]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [showJump, setShowJump] = useState(false);
  const [activeTick, setActiveTick] = useState(0);
  // The scrollbar's width: 0 with overlay scrollbars. A scrollbar appearing
  // shrinks the content box, which is what the observer sees.
  const [scrollbar, setScrollbar] = useState(0);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const measure = () => setScrollbar(el.offsetWidth - el.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  // Figma 951:2270 is opened from a message footer, so the transcript owns it:
  // one panel for whichever turn you asked about, not one per message.
  const [inspecting, setInspecting] = useState<TranscriptItem | null>(null);
  const isAtBottomRef = useRef(true);

  // Handle scroll events to detect if user has scrolled up away from latest
  function handleScroll() {
    const el = scrollRef.current;
    if (!el) return;
    const distanceToBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distanceToBottom < 80;
    isAtBottomRef.current = atBottom;
    setShowJump(!atBottom);
    setActiveTick(tickInView(el));
  }

  function sectionOf(id: string) {
    return scrollRef.current?.querySelector<HTMLElement>(`[data-turn-id="${CSS.escape(id)}"]`) ?? null;
  }

  // The prompt you are reading: the last one that has reached the top of the
  // reading area, just under the glass header — where a jump lands it. Scrolled
  // all the way down, a short last turn can never climb that high, so there it
  // is the last one showing above the composer.
  function tickInView(el: HTMLDivElement): number {
    const box = el.getBoundingClientRect();
    const style = getComputedStyle(el);
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight < 2;
    const line = atEnd
      ? box.bottom - parseFloat(style.paddingBottom)
      : box.top + parseFloat(style.paddingTop) + 8;
    let found = 0;
    ticks.forEach((tick, index) => {
      const section = sectionOf(tick.id);
      if (section && section.getBoundingClientRect().top <= line) found = index;
    });
    return found;
  }

  // Scrolls the transcript alone. scrollIntoView would also scroll every
  // overflow-hidden ancestor up to the window, shifting the hub's layout.
  function jumpTo(id: string) {
    const el = scrollRef.current;
    const section = sectionOf(id);
    if (!el || !section) return;
    const margin = parseFloat(getComputedStyle(section).scrollMarginTop) || 0;
    const offset = section.getBoundingClientRect().top - el.getBoundingClientRect().top;
    el.scrollTo({ top: el.scrollTop + offset - margin });
  }

  // Auto-scroll on new items if user is already at the bottom
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (isAtBottomRef.current) {
      el.scrollTop = el.scrollHeight;
      setActiveTick(tickInView(el));
    }
  }, [items, ticks.length]);

  function scrollToBottom() {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    setShowJump(false);
  }

  return (
    <div
      className={cn("relative flex size-full min-h-0 flex-col overflow-hidden", className)}
      data-testid="transcript-stream"
    >
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        data-testid="transcript-viewport"
        className="flex size-full min-h-0 flex-col overflow-y-auto px-6 scroll-smooth"
        // Clear of the glass header above and the floating composer below: both
        // sit over this scroller, and AgentHubContent measures each into a
        // variable so the first and last lines can scroll out from under them.
        style={{
          // Measured scrollbar width, for the column's re-centring below.
          "--scrollbar": `${scrollbar}px`,
          paddingTop: "calc(var(--header-inset, 0px) + var(--space-section))",
          paddingBottom: "calc(var(--composer-inset, 0px) + var(--space-section))",
        } as CSSProperties}
      >
        <div
          className="relative mx-auto flex w-full max-w-180 flex-col gap-6"
          // A classic scrollbar (macOS "Show scroll bars: Always", a mouse
          // plugged in, Windows) takes its width off the right only, so a
          // capped column centres half a scrollbar left of the composer, which
          // centres in the whole pane. Shift it back by that half — but only
          // out of the slack beside a capped column. A column filling a narrow
          // pane (the agent rail) stays put and keeps every pixel.
          // `both-edges` gutters did the centring by reserving the width on the
          // left too, which in a narrow panel was pure lost space.
          style={{
            left: "clamp(0px, calc((100% - var(--spacing) * 180) / 2), calc(var(--scrollbar, 0px) / 2))",
          }}
        >
          {items.length === 0 && !loading ? (
            <div className="flex h-48 flex-col items-center justify-center text-center">
              <p className="text-body-13 text-text-secondary">No messages yet</p>
              <p className="text-caption-11 text-text-tertiary">
                Send a prompt below to start interacting with the agent.
              </p>
            </div>
          ) : null}

          {turns.map((turn, index) => {
            const latest = index === turns.length - 1;
            return (
              <AgentTurn
                key={turn.id}
                turn={turn}
                latest={latest}
                live={latest && streaming}
                vendor={vendor}
                onInspect={setInspecting}
                onCopy={onCopy}
                onEditPrompt={onEditPrompt}
              />
            );
          })}
        </div>
      </div>

      <TranscriptTicks
        ticks={ticks}
        active={activeTick}
        onJump={jumpTo}
        className="absolute top-1/2 left-2 z-20 -translate-y-1/2"
      />

      <AgentMetaInspector
        open={Boolean(inspecting)}
        onOpenChange={(next) => {
          if (!next) setInspecting(null);
        }}
        agentName={vendor}
        stopReason={metaField(inspecting, "stopReason")}
        cost={metaNumber(inspecting, "cost")}
        inputTokens={metaNumber(inspecting, "inputTokens")}
        outputTokens={metaNumber(inspecting, "outputTokens")}
        meta={inspecting?.raw}
      />

      {/* Floating Jump to Latest Button */}
      <div
        className="pointer-events-none absolute left-1/2 z-20 -translate-x-1/2"
        style={{ bottom: "calc(var(--composer-inset, 0px) + var(--space-control))" }}
      >
        <div className="pointer-events-auto">
          <AgentJumpToLatest visible={showJump} onClick={scrollToBottom} />
        </div>
      </div>
    </div>
  );
}

/** The raw envelope, when there is one to read. */
function rawOf(item: TranscriptItem | null | undefined): Record<string, unknown> | undefined {
  return item && typeof item.raw === "object" && item.raw !== null
    ? (item.raw as Record<string, unknown>)
    : undefined;
}

function metaField(item: TranscriptItem | null | undefined, key: string): string | undefined {
  const value = rawOf(item)?.[key];
  return typeof value === "string" ? value : undefined;
}

function metaNumber(item: TranscriptItem | null | undefined, key: string): number | undefined {
  const value = rawOf(item)?.[key];
  return typeof value === "number" ? value : undefined;
}
