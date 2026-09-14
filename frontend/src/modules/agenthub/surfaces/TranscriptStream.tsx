import { useEffect, useMemo, useRef, useState } from "react";
import { ScrollArea as ScrollAreaPrimitive } from "radix-ui";
import { ScrollBar } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import type { TranscriptItem } from "../types";
import { groupTurns, type Turn } from "../turns";
import { AgentJumpToLatest } from "./AgentJumpToLatest";
import { AgentMetaInspector } from "./AgentMetaInspector";
import { AgentTurn } from "./AgentTurn";
import { TranscriptTicks, ticksOf } from "./TranscriptTicks";

export type TranscriptStreamProps = {
  items: TranscriptItem[];
  /** Resets structural sharing when the visible session changes. */
  streamKey?: string;
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
  streamKey,
  vendor = "Claude",
  streaming = false,
  onCopy,
  onEditPrompt,
  loading = false,
  className,
}: TranscriptStreamProps) {
  const previousTurnsRef = useRef<Turn[]>([]);
  const previousTicksRef = useRef<ReturnType<typeof ticksOf>>([]);
  const previousStreamKeyRef = useRef<string | undefined>(streamKey);
  const turns = useMemo(() => {
    const previous = previousStreamKeyRef.current === streamKey ? previousTurnsRef.current : [];
    previousStreamKeyRef.current = streamKey;
    if (previous.length === 0) previousTicksRef.current = [];
    return groupTurns(items, previous);
  }, [items, streamKey]);
  const ticks = useMemo(() => {
    const next = ticksOf(turns, previousTurnsRef.current, previousTicksRef.current);
    previousTurnsRef.current = turns;
    previousTicksRef.current = next;
    return next;
  }, [turns]);

  const onCopyRef = useRef(onCopy);
  const onEditPromptRef = useRef(onEditPrompt);
  onCopyRef.current = onCopy;
  onEditPromptRef.current = onEditPrompt;
  const stableOnCopy = useMemo(
    () => (onCopy ? (text: string) => onCopyRef.current?.(text) : undefined),
    [Boolean(onCopy)],
  );
  const stableOnEditPrompt = useMemo(
    () => (onEditPrompt ? (text: string) => onEditPromptRef.current?.(text) : undefined),
    [Boolean(onEditPrompt)],
  );
  const scrollRef = useRef<HTMLDivElement>(null);
  const [showJump, setShowJump] = useState(false);
  const [activeTick, setActiveTick] = useState(0);
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
      {/* An overlay scrollbar (Radix ScrollArea) rather than the native one.
          A native scrollbar takes width whenever the OS shows it permanently —
          macOS "Show scroll bars: Always", a mouse plugged in, Windows, most
          Linux themes — and CSS cannot turn that off: the column lost that
          width and slid off-centre from the composer, which is centred in the
          whole pane. This one floats over the content's right edge and takes
          none, on every platform, and shows while scrolling or hovered. */}
      <ScrollAreaPrimitive.Root type="hover" className="size-full min-h-0">
      <ScrollAreaPrimitive.Viewport
        ref={scrollRef}
        onScroll={handleScroll}
        data-testid="transcript-viewport"
        // Radix wraps the content in a display:table div so it can scroll
        // sideways too; as a table it widens to fit a long code line and drags
        // the column past the pane. Block keeps it to the viewport's width.
        className="size-full px-6 scroll-smooth [&>div]:block!"
        // Clear of the glass header above and the floating composer below: both
        // sit over this scroller, and AgentHubContent measures each into a
        // variable so the first and last lines can scroll out from under them.
        style={{
          paddingTop: "calc(var(--header-inset, 0px) + var(--space-section))",
          paddingBottom: "calc(var(--composer-inset, 0px) + var(--space-section))",
        }}
      >
        <div className="mx-auto flex w-full max-w-180 flex-col gap-6">
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
                onCopy={stableOnCopy}
                onEditPrompt={stableOnEditPrompt}
              />
            );
          })}
        </div>
      </ScrollAreaPrimitive.Viewport>
      {/* Clear of the header and composer, which float over both ends. Top and
          bottom rather than margins: Radix positions the bar absolutely and
          the class gives it full height, so a margin only slid it down. */}
      <ScrollBar
        style={{ top: "var(--header-inset, 0px)", bottom: "var(--composer-inset, 0px)", height: "auto" }}
      />
      </ScrollAreaPrimitive.Root>

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
