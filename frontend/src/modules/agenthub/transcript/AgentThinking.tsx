import * as React from "react";
import { ChevronDown, ChevronRight, Sparkles } from "lucide-react";
import { cn } from "@/lib/utils";

export type AgentThinkingProps = {
  /**
   * Reasoning / thought block.
   * Figma node: 1029:2742 (Agent / Thinking)
   * Variants: Collapsed / Expanded
   */
  text: string;
  duration?: string;
  expanded?: boolean;
  defaultExpanded?: boolean;
  onToggleExpanded?: () => void;
  className?: string;
};

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
    if (onToggleExpanded) {
      onToggleExpanded();
    } else {
      setInternalExpanded((prev) => !prev);
    }
  };

  return (
    <div
      className={cn(
        "flex flex-col w-full max-w-full rounded-md border border-border-subtle bg-surface-base/60 overflow-hidden",
        className,
      )}
      data-testid="agent-thinking"
      data-state={isExpanded ? "expanded" : "collapsed"}
    >
      <button
        type="button"
        onClick={toggle}
        className="flex h-(--control-height-md) shrink-0 items-center justify-between gap-2 px-3 text-left hover:bg-surface-raised/40 transition-colors"
      >
        <div className="flex items-center gap-2 min-w-0">
          <Sparkles className="size-(--icon-14) text-text-tertiary shrink-0" />
          <span className="truncate text-caption-11-medium text-text-tertiary">
            {`Thinking${duration ? ` (${duration})` : "..."}`}
          </span>
        </div>

        {isExpanded ? (
          <ChevronDown className="size-(--icon-12) text-text-tertiary shrink-0" />
        ) : (
          <ChevronRight className="size-(--icon-12) text-text-tertiary shrink-0" />
        )}
      </button>

      {isExpanded && (
        <div className="border-t border-border-subtle p-3 text-caption-11 text-text-secondary select-text whitespace-pre-wrap overflow-x-auto">
          {text}
        </div>
      )}
    </div>
  );
}
