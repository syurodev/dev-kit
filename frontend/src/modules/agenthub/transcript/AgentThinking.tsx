import * as React from "react";
import { ChevronDown, ChevronRight } from "lucide-react";

import { cn } from "@/lib/utils";

export type AgentThinkingProps = {
  text: string;
  /** "6s" — Figma reads it back as "Thought for 6s", past tense. */
  duration?: string;
  expanded?: boolean;
  defaultExpanded?: boolean;
  onToggleExpanded?: () => void;
  className?: string;
};

/**
 * Agent / Thinking — Figma `1029:2742`, collapsed state `1029:2731`.
 *
 * The quietest thing in the transcript: state/soft, no border, one line of
 * Caption/11 in the tertiary tier. It is a receipt that reasoning happened, and
 * it sits between things you actually need to read — a bordered card at
 * surface/base gave it the same weight as a tool call.
 *
 * "Thought for 6s", not "Thinking…": by the time the chunk is in the transcript
 * the thinking is over, and a present-tense label on a finished block reads as
 * a stuck spinner.
 */
export function AgentThinking({
  text,
  duration,
  expanded: controlledExpanded,
  defaultExpanded = false,
  onToggleExpanded,
  className,
}: AgentThinkingProps) {
  const [internalExpanded, setInternalExpanded] = React.useState(defaultExpanded);
  const isExpanded = controlledExpanded ?? internalExpanded;

  const toggle = () => {
    if (onToggleExpanded) onToggleExpanded();
    else setInternalExpanded((prev) => !prev);
  };

  return (
    <div
      data-testid="agent-thinking"
      data-state={isExpanded ? "expanded" : "collapsed"}
      className={cn(
        "flex w-full max-w-full flex-col gap-2 rounded-control bg-(--state-soft) px-3 py-2",
        className,
      )}
    >
      <button
        type="button"
        onClick={toggle}
        aria-expanded={isExpanded}
        className="flex w-full items-center gap-2 text-left cursor-pointer"
      >
        {isExpanded ? (
          <ChevronDown className="size-3 shrink-0 text-text-tertiary" aria-hidden="true" />
        ) : (
          <ChevronRight className="size-3 shrink-0 text-text-tertiary" aria-hidden="true" />
        )}
        <span className="min-w-0 flex-1 truncate text-caption-11 text-text-tertiary">
          {duration ? `Thought for ${duration}` : "Thought"}
        </span>
      </button>

      {isExpanded ? (
        <p className="w-full overflow-x-auto text-caption-11 whitespace-pre-wrap text-text-secondary select-text">
          {text}
        </p>
      ) : null}
    </div>
  );
}
