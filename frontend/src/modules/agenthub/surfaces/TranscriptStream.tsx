import { useEffect, useRef, useState } from "react";
import { UpdateKind } from "../../../../bindings/devkit/internal/agenthub/models";
import { cn } from "@/lib/utils";
import type { TranscriptItem } from "../types";
import {
  AgentMessage,
  AgentThinking,
  AgentToolCall,
  AgentPlan,
  AgentInvokeResult,
  AgentRateLimit,
} from "../transcript/index";
import { AgentJumpToLatest } from "./AgentJumpToLatest";

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
  const isAtBottomRef = useRef(true);

  // Handle scroll events to detect if user has scrolled up away from latest
  function handleScroll() {
    const el = scrollRef.current;
    if (!el) return;
    const distanceToBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distanceToBottom < 80;
    isAtBottomRef.current = atBottom;
    setShowJump(!atBottom);
  }

  // Auto-scroll on new items if user is already at the bottom
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (isAtBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [items]);

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
      {/* Scrollable Viewport */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        data-testid="transcript-viewport"
        className="flex size-full min-h-0 flex-col gap-4 overflow-y-auto px-4 py-4 scroll-smooth"
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
            return (
              <AgentThinking
                key={item.id}
                text={item.text || ""}
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

          // Default / Agent message chunk
          return (
            <AgentMessage
              key={item.id}
              align="start"
              author={vendor}
              text={item.text || (typeof item.raw === "string" ? item.raw : "")}
            />
          );
        })}
      </div>

      {/* Floating Jump to Latest Button */}
      <div className="pointer-events-none absolute bottom-3 left-1/2 -translate-x-1/2 z-20">
        <div className="pointer-events-auto">
          <AgentJumpToLatest
            visible={showJump}
            onClick={scrollToBottom}
          />
        </div>
      </div>
    </div>
  );
}
