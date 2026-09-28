import { Shimmer } from "@/components/ui/shimmer";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useReducedMotion } from "framer-motion";
import { ScrollArea as ScrollAreaPrimitive } from "radix-ui";
import { ScrollBar } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { scheduleTranscriptFrame } from "../transcript";
import type { TranscriptItem } from "../types";
import { groupTurns, type Turn } from "../turns";
import { AgentJumpToLatest } from "./AgentJumpToLatest";
import { AgentMetaInspector } from "./AgentMetaInspector";
import { AgentTurn } from "./AgentTurn";
import { EMPTY_CHECKPOINTS, type CheckpointIndex } from "../checkpoints";
import type { FileDiff } from "../../../../bindings/devkit/internal/agenthub/models";
import { TranscriptTicks, ticksOf } from "./TranscriptTicks";
import { vendorInfo } from "../vendors";

export type TranscriptStreamProps = {
  items: TranscriptItem[];
  /** Resets structural sharing when the visible session changes. */
  streamKey?: string;
  /** The vendor, for marks and metadata — not what a message is signed with. */
  vendor?: string;
  /**
   * The session's file checkpoints, keyed on the prompt that started each turn.
   * Turns with no entry draw no count.
   */
  checkpoints?: CheckpointIndex;
  /** Loads one changed file's two versions, when its row is opened. */
  loadFileDiff?: (checkpointId: string, path: string) => Promise<FileDiff>;
  /** Opens the confirmation for taking the session back to a message. */
  onRewind?: (promptMessageId: string, editAfter: boolean) => void;
  /** A turn is in flight. The newest turn runs its orb and follows its steps. */
  streaming?: boolean;
  /** Copy, in each message's hover card. */
  onCopy?: (text: string) => void;
  /** Edit, in your own prompts' hover cards: the text goes back in the composer. */
  onEditPrompt?: (text: string) => void;
  onOpenFile?: (path: string, line?: number) => void;
  loading?: boolean;
  /** Older pages exist above the ones held (see useAgentTranscript). */
  hasMore?: boolean;
  /** An older page is in flight; the top pill says so instead of going quiet. */
  loadingOlder?: boolean;
  /** Walks one page further back. The stream anchors the scroll around it. */
  onLoadOlder?: () => void;
  className?: string;
};

/**
 * The scroll position a prepend has to preserve: the reader stays on the
 * line they are on while the older page lands above it, so the viewport
 * moves down by exactly the height that was inserted.
 */
export function anchoredScrollTop(
  prevScrollTop: number,
  prevScrollHeight: number,
  nextScrollHeight: number,
): number {
  return prevScrollTop + (nextScrollHeight - prevScrollHeight);
}

/** The viewport, and the oldest item held, when an older page is asked for. */
export type PrependAnchor = { scrollTop: number; scrollHeight: number; firstItemId?: string };

/**
 * One commit's step for a held prepend anchor. The request turns loadingOlder
 * on before any page exists, so while it is in flight the anchor only follows
 * the viewport — a token landing below and the reader's own scrolling are not
 * the prepend. Once it settles the viewport moves down by what landed above;
 * a page that failed (the oldest item is unchanged) just drops the anchor so
 * the next approach to the top asks again.
 */
export function settlePrependAnchor(
  anchor: PrependAnchor | null,
  loadingOlder: boolean,
  now: PrependAnchor,
): { anchor: PrependAnchor | null; scrollTop: number | null } {
  if (!anchor) return { anchor: null, scrollTop: null };
  if (loadingOlder) return { anchor: { ...now, firstItemId: anchor.firstItemId }, scrollTop: null };
  const landed = now.firstItemId !== anchor.firstItemId && now.scrollHeight > anchor.scrollHeight;
  return {
    anchor: null,
    scrollTop: landed ? anchoredScrollTop(anchor.scrollTop, anchor.scrollHeight, now.scrollHeight) : null,
  };
}

export function isTranscriptAtBottom(
  scrollHeight: number,
  scrollTop: number,
  clientHeight: number,
  tolerance = 80,
): boolean {
  return scrollHeight - scrollTop - clientHeight < tolerance;
}

/**
 * Whether the stream keeps following new output after a scroll sample. A
 * flush can grow the transcript past the tolerance before the auto-scroll
 * frame catches up, and a sample taken in between (often the previous
 * auto-scroll's own scroll event) reads that as "not at the bottom". Growth
 * moves nobody, so only the reader turns following off: input — wheel, touch,
 * keyboard, the scrollbar — that moved the view up.
 */
export function keepsFollowing(
  following: boolean,
  sample: { atBottom: boolean; scrollTop: number; previousScrollTop: number; userScrolled: boolean },
): boolean {
  if (sample.atBottom) return true;
  return following && !(sample.userScrolled && sample.scrollTop < sample.previousScrollTop);
}

export function transcriptBottomScrollTop(scrollHeight: number, clientHeight: number): number {
  return Math.max(0, scrollHeight - clientHeight);
}

/** How close to the top the older page is asked for. */
const PRELOAD_MARGIN_PX = 120;

/** Transcript turns are variable-height, so large histories need real windowing. */
export const TRANSCRIPT_VIRTUALIZE_THRESHOLD = 300;
export const TRANSCRIPT_OVERSCAN = 6;

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
  vendor = "",
  checkpoints = EMPTY_CHECKPOINTS,
  loadFileDiff,
  onRewind,
  streaming = false,
  onCopy,
  onEditPrompt,
  onOpenFile,
  loading = false,
  hasMore = false,
  loadingOlder = false,
  onLoadOlder,
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
  const onOpenFileRef = useRef(onOpenFile);
  const onRewindRef = useRef(onRewind);
  onCopyRef.current = onCopy;
  onEditPromptRef.current = onEditPrompt;
  onOpenFileRef.current = onOpenFile;
  onRewindRef.current = onRewind;
  const stableOnCopy = useMemo(
    () => (onCopy ? (text: string) => onCopyRef.current?.(text) : undefined),
    [Boolean(onCopy)],
  );
  const stableOnEditPrompt = useMemo(
    () => (onEditPrompt ? (text: string) => onEditPromptRef.current?.(text) : undefined),
    [Boolean(onEditPrompt)],
  );
  const stableOnOpenFile = useMemo(
    () => (onOpenFile ? (path: string, line?: number) => onOpenFileRef.current?.(path, line) : undefined),
    [Boolean(onOpenFile)],
  );
  // Both callers pass this as an inline arrow. Handed to every AgentTurn as
  // it came, it was a new function per streamed token, so memo let no turn
  // skip — a long session re-rendered all of its history on every token.
  const stableOnRewind = useMemo(
    () =>
      onRewind
        ? (promptMessageId: string, editAfter: boolean) => onRewindRef.current?.(promptMessageId, editAfter)
        : undefined,
    [Boolean(onRewind)],
  );
  const scrollRef = useRef<HTMLDivElement>(null);
  const reduce = useReducedMotion() ?? false;
  const [showJump, setShowJump] = useState(false);
  const [activeTick, setActiveTick] = useState(0);
  // Figma 951:2270 is opened from a message footer, so the transcript owns it:
  // one panel for whichever turn you asked about, not one per message.
  const [inspecting, setInspecting] = useState<TranscriptItem | null>(null);
  const isAtBottomRef = useRef(true);
  // Reader input since the last scroll sample, and where that sample was (keepsFollowing).
  const userScrollRef = useRef(false);
  const lastScrollTopRef = useRef(0);
  const markUserScroll = () => {
    userScrollRef.current = true;
  };
  const activeTickRef = useRef(0);
  const scrollFrameRef = useRef<(() => void) | null>(null);
  const autoScrollFrameRef = useRef<(() => void) | null>(null);
  const pendingJumpRef = useRef<string | null>(null);
  // The scroll geometry while an older page is on its way (settlePrependAnchor).
  // The layout effect below moves the viewport down by whatever the prepend
  // inserted, so the line being read does not jump.
  const prependAnchorRef = useRef<PrependAnchor | null>(null);

  const virtualized = turns.length > TRANSCRIPT_VIRTUALIZE_THRESHOLD;
  // virtual-core rebuilds every measurement (O(turns)) when the key function
  // changes identity, and a new closure per render did that on every flush.
  // The keys only change when turns are prepended, appended or replaced, and
  // each of those moves an edge or the count.
  const firstTurnId = turns[0]?.id;
  const lastTurnId = turns[turns.length - 1]?.id;
  const getItemKey = useMemo(() => {
    const ids = turns.map((turn) => turn.id);
    return (index: number) => ids[index] ?? index;
  }, [turns.length, firstTurnId, lastTurnId]);
  // The rows start below the viewport's top padding (the glass header's
  // inset), so the virtualizer's window is offset by it. A padding change
  // resizes the content box, which is what the observer sees.
  const [scrollMargin, setScrollMargin] = useState(0);
  useEffect(() => {
    const el = scrollRef.current;
    if (!virtualized || !el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => setScrollMargin(parseFloat(getComputedStyle(el).paddingTop) || 0));
    observer.observe(el);
    return () => observer.disconnect();
  }, [virtualized]);
  const rowVirtualizer = useVirtualizer({
    count: virtualized ? turns.length : 0,
    getScrollElement: () => scrollRef.current,
    getItemKey,
    scrollMargin,
    estimateSize: () => 180,
    overscan: TRANSCRIPT_OVERSCAN,
    paddingStart: virtualized && hasMore && items.length > 0 ? 40 : 0,
    initialRect: { width: 0, height: 800 },
  });
  const virtualItems = rowVirtualizer.getVirtualItems();
  const mountedTurnKey = virtualized
    ? virtualItems.map((item) => `${item.index}:${turns[item.index]?.id ?? ""}`).join(",")
    : turns.map((turn) => turn.id).join(",");
  const tickIndexById = useMemo(() => new Map(ticks.map((tick, index) => [tick.id, index])), [ticks]);
  // Read by the section observer, which must not be rebuilt when only a tick's preview changes.
  const tickIndexByIdRef = useRef(tickIndexById);
  tickIndexByIdRef.current = tickIndexById;

  function setActiveTickIfChanged(next: number) {
    if (activeTickRef.current === next) return;
    activeTickRef.current = next;
    setActiveTick(next);
  }

  // The single source of truth for "are we following", read live off the DOM
  // rather than trusting whichever frame last wrote isAtBottomRef. The
  // auto-scroll effect below and this handler both schedule a rAF, and on a
  // busy stream (a flush per token) those two frames land in either order.
  // A stale ref would let auto-scroll win the race: it runs first, reads the
  // *previous* isAtBottomRef (still true), snaps to the bottom, and only then
  // does handleScroll's frame run — sampling a scrollTop the snap already put
  // back at the bottom, so it reads "at bottom" and turns following back on,
  // erasing the scroll-up as if it never happened. Recomputing from the live
  // element here means whichever frame runs first sees the same answer.
  function sampleFollowing(el: HTMLDivElement): boolean {
    const scrollTop = el.scrollTop;
    const following = keepsFollowing(isAtBottomRef.current, {
      atBottom: isTranscriptAtBottom(el.scrollHeight, scrollTop, el.clientHeight),
      scrollTop,
      previousScrollTop: lastScrollTopRef.current,
      userScrolled: userScrollRef.current,
    });
    lastScrollTopRef.current = scrollTop;
    userScrollRef.current = false;
    isAtBottomRef.current = following;
    return following;
  }

  // Scroll state is sampled once per frame. Reading the three scroll metrics in
  // the event handler made a burst of wheel events compete with layout.
  function handleScroll() {
    if (scrollFrameRef.current) return;
    scrollFrameRef.current = scheduleTranscriptFrame(() => {
      scrollFrameRef.current = null;
      const el = scrollRef.current;
      if (!el) return;
      const following = sampleFollowing(el);
      setShowJump((previous) => (previous === !following ? previous : !following));
      if (following && ticks.length > 0) setActiveTickIfChanged(ticks.length - 1);
      const anchor = prependAnchorRef.current;
      if (anchor) {
        // Waiting on a page: the reader is still reading, so the anchor moves with them.
        prependAnchorRef.current = { ...anchor, scrollTop: el.scrollTop, scrollHeight: el.scrollHeight };
      } else if (hasMore && !loadingOlder && el.scrollTop <= PRELOAD_MARGIN_PX) {
        requestOlder();
      }
    });
  }

  // The pill and the preload both come through here: a page asked for without
  // an anchor lands with nothing to keep the reader's line in place.
  function requestOlder() {
    const el = scrollRef.current;
    if (!el || !onLoadOlder) return;
    prependAnchorRef.current = { scrollTop: el.scrollTop, scrollHeight: el.scrollHeight, firstItemId: items[0]?.id };
    onLoadOlder();
  }

  // Keeps the reader where they were across a prepend. Layout effect, before
  // paint: without it the browser paints the un-compensated scroll first and
  // the transcript visibly jumps. The scrollTop written is absolute, so a
  // browser that anchors scroll itself (Chromium) is overwritten, not added to.
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!prependAnchorRef.current || !el) return;
    const step = settlePrependAnchor(prependAnchorRef.current, loadingOlder, {
      scrollTop: el.scrollTop,
      scrollHeight: el.scrollHeight,
      firstItemId: items[0]?.id,
    });
    prependAnchorRef.current = step.anchor;
    if (step.scrollTop !== null) el.scrollTop = step.scrollTop;
  }, [items, loadingOlder]);

  function sectionOf(id: string) {
    return scrollRef.current?.querySelector<HTMLElement>(`[data-turn-id="${CSS.escape(id)}"]`) ?? null;
  }

  function scrollToSection(id: string, behavior: ScrollBehavior) {
    const el = scrollRef.current;
    const section = sectionOf(id);
    if (!el || !section) return;
    const margin = parseFloat(getComputedStyle(section).scrollMarginTop) || 0;
    const offset = section.getBoundingClientRect().top - el.getBoundingClientRect().top;
    el.scrollTo({ top: el.scrollTop + offset - margin, behavior });
  }

  // A tick may point at a virtualized row. Bring that index into the window,
  // then adjust for the glass header after React has mounted its section.
  function jumpTo(id: string) {
    const index = turns.findIndex((turn) => turn.id === id);
    if (index < 0) return;
    // A jump back to an earlier prompt is the reader leaving the bottom; the
    // next scroll sample turns following back on if the target is the bottom.
    isAtBottomRef.current = false;
    pendingJumpRef.current = id;
    if (virtualized) {
      rowVirtualizer.scrollToIndex(index, { align: "start", behavior: "auto" });
      return;
    }
    pendingJumpRef.current = null;
    scrollToSection(id, reduce ? "instant" : "smooth");
  }

  // Observe only mounted prompt sections. The narrow root band replaces a
  // full transcript query + getBoundingClientRect scan on every scroll event.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || typeof IntersectionObserver === "undefined") return;
    const headerInset = parseFloat(getComputedStyle(el).getPropertyValue("--header-inset")) || 0;
    const visible = new Map<string, number>();
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const id = (entry.target as HTMLElement).dataset.turnId;
          if (!id) continue;
          if (entry.isIntersecting) visible.set(id, entry.boundingClientRect.top);
          else visible.delete(id);
        }
        const indexById = tickIndexByIdRef.current;
        if (isAtBottomRef.current && indexById.size > 0) {
          setActiveTickIfChanged(indexById.size - 1);
          return;
        }
        let next = activeTickRef.current;
        let top = Number.POSITIVE_INFINITY;
        for (const [id, value] of visible) {
          const index = indexById.get(id);
          if (index !== undefined && value < top) {
            top = value;
            next = index;
          }
        }
        setActiveTickIfChanged(next);
      },
      { root: el, rootMargin: `-${headerInset}px 0px -70% 0px`, threshold: [0, 1] },
    );
    el.querySelectorAll<HTMLElement>("[data-turn-id]").forEach((section) => observer.observe(section));
    return () => observer.disconnect();
  }, [mountedTurnKey]);

  useEffect(() => {
    const id = pendingJumpRef.current;
    if (!id || !sectionOf(id)) return;
    pendingJumpRef.current = null;
    const cancel = scheduleTranscriptFrame(() => scrollToSection(id, reduce ? "instant" : "smooth"));
    return cancel;
  }, [mountedTurnKey, reduce]);

  // Auto-scroll after a store flush, at most once in the next frame. The
  // instant write avoids restarting a smooth animation for every token.
  useEffect(() => {
    autoScrollFrameRef.current?.();
    autoScrollFrameRef.current = scheduleTranscriptFrame(() => {
      autoScrollFrameRef.current = null;
      const el = scrollRef.current;
      // Resample rather than trust isAtBottomRef: a wheel/touch/key input
      // this same frame (see sampleFollowing) must be able to cancel this
      // snap, not just the next handleScroll frame after it.
      if (!el || !sampleFollowing(el)) return;
      el.scrollTo({ top: transcriptBottomScrollTop(el.scrollHeight, el.clientHeight), behavior: "instant" });
      lastScrollTopRef.current = el.scrollTop;
      if (ticks.length > 0) setActiveTickIfChanged(ticks.length - 1);
    });
    return () => {
      autoScrollFrameRef.current?.();
      autoScrollFrameRef.current = null;
    };
  }, [items, ticks.length]);

  useEffect(() => () => {
    scrollFrameRef.current?.();
    autoScrollFrameRef.current?.();
  }, []);

  function scrollToBottom() {
    const el = scrollRef.current;
    if (!el) return;
    isAtBottomRef.current = true;
    el.scrollTo({
      top: transcriptBottomScrollTop(el.scrollHeight, el.clientHeight),
      behavior: reduce ? "instant" : "smooth",
    });
    setShowJump((previous) => (previous ? false : previous));
    if (ticks.length > 0) setActiveTickIfChanged(ticks.length - 1);
  }

  function renderTurn(turn: Turn, index: number) {
    const latest = index === turns.length - 1;
    return (
      <AgentTurn
        key={turn.id}
        turn={turn}
        latest={latest}
        live={latest && streaming}
        vendor={vendor}
        checkpoint={turn.prompt?.messageId ? checkpoints.get(turn.prompt.messageId) : undefined}
        loadFileDiff={loadFileDiff}
        onRewind={stableOnRewind}
        onInspect={setInspecting}
        onCopy={stableOnCopy}
        onEditPrompt={stableOnEditPrompt}
        onOpenFile={stableOnOpenFile}
      />
    );
  }

  function renderVirtualTurn(item: (typeof virtualItems)[number]) {
    const turn = turns[item.index];
    if (!turn) return null;
    return (
      <div
        key={turn.id}
        ref={rowVirtualizer.measureElement}
        data-index={item.index}
        data-testid="transcript-virtual-row"
        className="absolute top-0 left-0 w-full pb-6"
        // start counts the scroll margin; the row is placed inside the padded content.
        style={{ transform: `translateY(${item.start - scrollMargin}px)` }}
      >
        {renderTurn(turn, item.index)}
      </div>
    );
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
        onWheel={markUserScroll}
        onTouchMove={markUserScroll}
        onKeyDown={markUserScroll}
        data-testid="transcript-viewport"
        // Radix wraps the content in a display:table div so it can scroll
        // sideways too; as a table it widens to fit a long code line and drags
        // the column past the pane. Block keeps it to the viewport's width.
        className="size-full px-6 [&>div]:block!"
        // Clear of the glass header above and the floating composer below: both
        // sit over this scroller, and AgentHubContent measures each into a
        // variable so the first and last lines can scroll out from under them.
        style={{
          paddingTop: "calc(var(--header-inset, 0px) + var(--space-section))",
          paddingBottom: "calc(var(--composer-inset, 0px) + var(--space-section))",
        }}
      >
        <div
          className={cn("relative mx-auto w-full max-w-180", !virtualized && "flex flex-col gap-6")}
          style={virtualized ? { height: rowVirtualizer.getTotalSize() } : undefined}
        >
          {/* History is still above and the transcript says so — a silently
              truncated conversation reads as complete and lies about what
              the agent said. Reachable by scrolling; the pill is for the
              first discovery and for when the preload has not fired. */}
          {virtualized && hasMore && items.length > 0 ? (
            <button
              type="button"
              onClick={requestOlder}
              disabled={loadingOlder || !onLoadOlder}
              data-testid="transcript-load-older"
              className={cn(
                "absolute top-0 left-1/2 z-10 -translate-x-1/2 inline-flex items-center gap-2 rounded-full",
                "border border-border-subtle bg-surface-base",
                "px-4 py-1 text-caption-11 text-text-tertiary",
                "cursor-pointer select-none",
                "hover:text-text-secondary focus-visible:ring-ring focus-visible:ring-1 focus-visible:outline-none",
                "disabled:cursor-default disabled:opacity-70",
              )}
            >
              <Shimmer active={loadingOlder}>{loadingOlder ? "Loading earlier messages…" : "Load earlier messages"}</Shimmer>
            </button>
          ) : null}

          {!virtualized && hasMore && items.length > 0 ? (
            <button
              type="button"
              onClick={requestOlder}
              disabled={loadingOlder || !onLoadOlder}
              data-testid="transcript-load-older"
              className={cn(
                "mx-auto inline-flex items-center gap-2 rounded-full",
                "border border-border-subtle bg-surface-base",
                "px-4 py-1 text-caption-11 text-text-tertiary",
                "cursor-pointer select-none",
                "hover:text-text-secondary focus-visible:ring-ring focus-visible:ring-1 focus-visible:outline-none",
                "disabled:cursor-default disabled:opacity-70",
              )}
            >
              <Shimmer active={loadingOlder}>{loadingOlder ? "Loading earlier messages…" : "Load earlier messages"}</Shimmer>
            </button>
          ) : null}

          {items.length === 0 && !loading ? (
            <div className="flex h-48 flex-col items-center justify-center text-center">
              <p className="text-body-13 text-text-secondary">No messages yet</p>
              <p className="text-caption-11 text-text-tertiary">
                Send a prompt below to start interacting with the agent.
              </p>
            </div>
          ) : null}

          {virtualized ? virtualItems.map(renderVirtualTurn) : turns.map(renderTurn)}
        </div>
      </ScrollAreaPrimitive.Viewport>
      {/* Clear of the header and composer, which float over both ends. Top and
          bottom rather than margins: Radix positions the bar absolutely and
          the class gives it full height, so a margin only slid it down. */}
      <ScrollBar
        // Radix captures the pointer on the bar while its thumb is dragged.
        onPointerMove={(event) => {
          if (event.buttons) markUserScroll();
        }}
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
        agentName={vendorInfo(vendor).name}
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
