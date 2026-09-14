import { useEffect, useMemo, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import type { TranscriptItem } from "../types";
import { groupTurns } from "../turns";
import { AgentJumpToLatest } from "./AgentJumpToLatest";
import { AgentMetaInspector } from "./AgentMetaInspector";
import { AgentTurn } from "./AgentTurn";

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
  const scrollRef = useRef<HTMLDivElement>(null);
  const [showJump, setShowJump] = useState(false);
  const [unread, setUnread] = useState(0);
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
    if (atBottom) setUnread(0);
  }

  // Auto-scroll on new items if user is already at the bottom
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (isAtBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    } else {
      // Counted, not just flagged: "3 new messages" tells you whether
      // scrolling back down is worth it.
      setUnread((n) => n + 1);
    }
  }, [items]);

  function scrollToBottom() {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    setShowJump(false);
    setUnread(0);
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
        className="flex size-full min-h-0 flex-col overflow-y-auto px-6 pt-6 scroll-smooth"
        // Clear of the floating composer, whose height AgentHubContent measures.
        style={{ paddingBottom: "calc(var(--composer-inset, 0px) + var(--space-section))" }}
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
                onCopy={onCopy}
                onEditPrompt={onEditPrompt}
              />
            );
          })}
        </div>
      </div>

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
          <AgentJumpToLatest visible={showJump} count={unread} onClick={scrollToBottom} />
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
