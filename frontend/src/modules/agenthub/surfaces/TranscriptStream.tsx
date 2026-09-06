import { useEffect, useRef, useState } from "react";
import { UpdateKind } from "../../../../bindings/devkit/internal/agenthub/models";
import { cn } from "@/lib/utils";
import type { TranscriptItem } from "../types";
import {
  AgentMessage,
  AgentMessageRich,
  AgentThinking,
  AgentToolCall,
  AgentPlan,
  AgentInvokeResult,
  AgentRateLimit,
  parseMarkdown,
} from "../transcript/index";
import { AgentJumpToLatest } from "./AgentJumpToLatest";
import { AgentMetaInspector } from "./AgentMetaInspector";

export type TranscriptStreamProps = {
  items: TranscriptItem[];
  vendor?: string;
  loading?: boolean;
  className?: string;
};

/**
 * TranscriptStream
 * Renders an ACP conversation stream by dispatching TranscriptItem rows
 * to the 11 Phase 2 leaf components. Handles scroll position tracking
 * and the floating AgentJumpToLatest button.
 */
export function TranscriptStream({
  items,
  vendor = "Claude",
  loading = false,
  className,
}: TranscriptStreamProps) {
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
      {/* Scrollable Viewport */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        data-testid="transcript-viewport"
        className="flex size-full min-h-0 flex-col gap-5 overflow-y-auto p-5 scroll-smooth"
      >
        {items.length === 0 && !loading ? (
          <div className="flex h-48 flex-col items-center justify-center text-center">
            <p className="text-body-13 text-text-secondary">No messages yet</p>
            <p className="text-caption-11 text-text-tertiary">
              Send a prompt below to start interacting with the agent.
            </p>
          </div>
        ) : null}

        {items.map((item) => {
          const kind = item.kind;

          // User message chunk
          if (
            kind === UpdateKind.UpdateUserMessageChunk ||
            kind === "user_message_chunk"
          ) {
            return (
              <AgentMessage
                key={item.id}
                align="end"
                author="You"
                text={item.text || ""}
              />
            );
          }

          // Agent thought chunk
          if (
            kind === UpdateKind.UpdateAgentThoughtChunk ||
            kind === "agent_thought_chunk"
          ) {
            const thoughtRaw = item.raw as Record<string, any> | undefined;
            return (
              <AgentThinking
                key={item.id}
                text={item.text || ""}
                duration={thoughtRaw?.duration}
              />
            );
          }

          // Tool call update / execution
          if (
            kind === UpdateKind.UpdateToolCall ||
            kind === UpdateKind.UpdateToolCallUpdate ||
            kind === "tool_call" ||
            kind === "tool_call_update"
          ) {
            const toolCall = item.toolCall ?? {
              id: item.toolCallId || item.id,
              title: "Tool Execution",
              status: "completed",
            };
            return <AgentToolCall key={item.id} call={toolCall} />;
          }

          // Plan update
          if (kind === UpdateKind.UpdatePlan || kind === "plan") {
            return <AgentPlan key={item.id} entries={item.plan ?? []} />;
          }

          // Invoke result
          if (kind === "invoke_result") {
            const raw = item.raw as Record<string, any> | undefined;
            return (
              <AgentInvokeResult
                key={item.id}
                agentName={raw?.agentName || vendor}
                stopReason={raw?.stopReason}
                duration={raw?.duration}
                cost={raw?.cost}
                summary={item.text || raw?.summary || ""}
                filesChanged={raw?.filesChanged ?? []}
              />
            );
          }

          // Rate limit warning
          if (kind === "rate_limit") {
            const raw = item.raw as Record<string, any> | undefined;
            return (
              <AgentRateLimit
                key={item.id}
                level={raw?.level === "exhausted" ? "exhausted" : "warning"}
                message={item.text}
                resetTime={raw?.resetTime}
              />
            );
          }

          // Default / Agent message chunk. Agents answer in markdown, so a
          // reply with any structure at all goes to the rich variant — the
          // plain bubble was printing the source, bullets and asterisks and
          // all. One unstructured paragraph stays a bubble: that is a remark,
          // not a document, and Figma gives it the avatar.
          const body = item.text || (typeof item.raw === "string" ? item.raw : "");
          const blocks = parseMarkdown(body);
          const rich =
            blocks.length > 1 || (blocks.length === 1 && blocks[0].type !== "paragraph");

          return rich ? (
            <AgentMessageRich key={item.id} author={vendor} text={body} />
          ) : (
            <AgentMessage
              key={item.id}
              align="start"
              author={vendor}
              text={body}
              footer={turnMeta(item) ? (
                <button
                  type="button"
                  data-testid="message-open-meta"
                  onClick={() => setInspecting(item)}
                  className="cursor-pointer hover:text-text-primary"
                >
                  {turnMeta(item)}
                </button>
              ) : undefined}
            />
          );
        })}
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
      <div className="pointer-events-none absolute bottom-3 left-1/2 -translate-x-1/2 z-20">
        <div className="pointer-events-auto">
          <AgentJumpToLatest
            visible={showJump}
            count={unread}
            onClick={scrollToBottom}
          />
        </div>
      </div>
    </div>
  );
}

/** The raw envelope, when there is one to read. */
function rawOf(item: TranscriptItem | null | undefined): Record<string, any> | undefined {
  return item && typeof item.raw === "object" && item.raw !== null
    ? (item.raw as Record<string, any>)
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

/**
 * "1.2s · $0.03" — the footer affordance that opens the inspector. Nothing is
 * rendered when the turn carried no numbers, so the footer never becomes a
 * button with no label.
 */
function turnMeta(item: TranscriptItem): string | undefined {
  const raw = rawOf(item);
  if (!raw) return undefined;
  const cost = typeof raw.cost === "number" ? raw.cost : undefined;
  const parts = [
    typeof raw.duration === "string" ? raw.duration : undefined,
    cost !== undefined ? `$${cost.toFixed(cost < 0.01 ? 4 : 2)}` : undefined,
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : undefined;
}
