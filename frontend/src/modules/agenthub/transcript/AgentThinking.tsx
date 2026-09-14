import * as React from "react";
import { Brain, ChevronDown, ChevronRight } from "lucide-react";

import { cn } from "@/lib/utils";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";

export type AgentThinkingProps = {
  text: string;
  /** "6s" — Figma reads it back as "Thought for 6s", past tense. */
  duration?: string;
  expanded?: boolean;
  defaultExpanded?: boolean;
  onToggleExpanded?: () => void;
  /** `row` is one step inside a turn's work, beside tool calls drawn the same way. */
  variant?: "card" | "row";
  className?: string;
};

/**
 * Agent / Thinking — Figma `1029:2742`, collapsed state `1029:2731`.
 *
 * The quietest thing in the transcript: state/soft, no border, one line of
 * Caption/11 in the tertiary tier. It is a receipt that reasoning happened,
 * sitting between things you actually need to read — a bordered card at
 * surface/base would give it a tool call's weight.
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
  variant = "card",
  className,
}: AgentThinkingProps) {
  const [internalExpanded, setInternalExpanded] = React.useState(defaultExpanded);
  const isExpanded = controlledExpanded ?? internalExpanded;
  const label = duration ? `Thought for ${duration}` : "Thought";

  const toggle = () => {
    if (onToggleExpanded) onToggleExpanded();
    else setInternalExpanded((prev) => !prev);
  };

  if (variant === "row") {
    return (
      <div
        data-testid="agent-thinking"
        data-variant="row"
        data-state={isExpanded ? "expanded" : "collapsed"}
        className={cn("flex w-full min-w-0 flex-col", className)}
      >
        <button
          type="button"
          onClick={toggle}
          aria-expanded={isExpanded}
          className="flex w-full min-w-0 cursor-pointer items-center gap-2 text-left text-body-13"
        >
          <Brain className="size-3.5 shrink-0 text-icon-secondary" aria-hidden="true" />
          <span className="min-w-0 truncate text-text-secondary">{label}</span>
          <ChevronRight
            aria-hidden="true"
            className={cn(
              "size-3.5 shrink-0 text-text-tertiary transition-transform duration-(--motion-fast)",
              isExpanded && "rotate-90",
            )}
          />
        </button>
        <AnimatedCollapse open={isExpanded} className="pt-2">
          <p className="w-full pl-5.5 text-body-13 whitespace-pre-wrap break-words text-text-tertiary select-text">
            {text}
          </p>
        </AnimatedCollapse>
      </div>
    );
  }

  return (
    <div
      data-testid="agent-thinking"
      data-state={isExpanded ? "expanded" : "collapsed"}
      className={cn(
        "flex w-full max-w-full flex-col rounded-control bg-(--state-soft) px-3 py-2",
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
        <span className="min-w-0 flex-1 truncate text-caption-11 text-text-tertiary">{label}</span>
      </button>

      <AnimatedCollapse open={isExpanded} className="pt-2">
        <p className="w-full overflow-x-auto text-caption-11 whitespace-pre-wrap text-text-secondary select-text">
          {text}
        </p>
      </AnimatedCollapse>
    </div>
  );
}
