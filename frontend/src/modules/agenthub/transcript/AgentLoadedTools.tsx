import * as React from "react";
import { ChevronRight, Wrench } from "lucide-react";

import { cn } from "@/lib/utils";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { Shimmer } from "@/components/ui/shimmer";
import type { TranscriptItem } from "../types";
import { toolSearchTerms } from "../turns";
import { toolActivityState } from "./AgentToolCall";
import { cannotFinish, useSettleFacts } from "./toolSettle";

/**
 * A stretch of ToolSearch calls as one quiet row: "Loaded tools (3)", opening
 * onto what each one asked for. Claude loads a deferred tool with a ToolSearch
 * before using it, so a turn has dozens and each says nothing the next step
 * does not (turns.ts foldToolSearch). Folded rather than hidden: what the agent
 * did stays one click away.
 *
 * Quiet by design: the row is tertiary text on the page's own ground, with no
 * result slot, and a call that failed is not in it (it keeps its own row).
 */
export function AgentLoadedTools({ items, defaultOpen = false }: { items: TranscriptItem[]; defaultOpen?: boolean }) {
  const [open, setOpen] = React.useState(defaultOpen);
  const facts = useSettleFacts(items[0]?.origin);
  // Loading, while a search can still finish; "Loaded" once they all have, or can never.
  const loading =
    !cannotFinish(facts) && items.some((item) => toolActivityState(item.toolCall?.status) === "running");
  const count = items.length > 1 ? ` (${items.length})` : "";
  return (
    <div
      data-testid="loaded-tools"
      data-state={open ? "expanded" : "collapsed"}
      className="flex w-full min-w-0 flex-col"
    >
      <button
        type="button"
        data-testid="loaded-tools-toggle"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        className={cn(
          "flex w-full min-w-0 cursor-pointer items-center gap-2 text-left text-body-13 text-text-tertiary",
          "hover:text-text-secondary focus-visible:ring-ring focus-visible:ring-1 focus-visible:outline-none",
        )}
      >
        <Wrench className="size-3.5 shrink-0 text-icon-secondary" aria-hidden="true" />
        <Shimmer active={loading} className="min-w-0 truncate">{`${loading ? "Loading tools" : "Loaded tools"}${count}`}</Shimmer>
        <ChevronRight
          aria-hidden="true"
          className={cn("size-3.5 shrink-0 transition-transform duration-(--motion-fast)", open && "rotate-90")}
        />
      </button>
      <AnimatedCollapse open={open} className="flex flex-col gap-1 pt-2 pl-5.5">
        {items.map((item) => {
          const { select, terms } = toolSearchTerms(item.toolCall!);
          return (
            <span
              key={item.id}
              data-testid="loaded-tools-call"
              className="min-w-0 truncate text-caption-11 text-text-tertiary"
            >
              {terms.length > 0 ? terms.join(select ? ", " : " ") : "Tool search"}
            </span>
          );
        })}
      </AnimatedCollapse>
    </div>
  );
}
