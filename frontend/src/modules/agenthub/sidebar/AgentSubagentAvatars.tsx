import * as React from "react";
import { Bot } from "lucide-react";

import { Avatar, AvatarFallback, AvatarGroup } from "@/components/ui/avatar";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { Subagent } from "../types";
import { initials, orderSubagents, subagentLabel, subagentName, subagentsLabel, subagentState, subagentStateWord, type SubagentState } from "./model";

/**
 * A running subagent reads as info, a failed one as danger, a finished one as
 * quiet. Colour is the second signal: the tooltip, the list and the group's
 * accessible name all say the state in words.
 */
const AVATAR_TONE: Record<SubagentState, string> = {
  running: "bg-status-info/15 text-status-info",
  failed: "bg-status-danger/15 text-status-danger",
  done: "bg-surface-raised text-text-tertiary",
};

/** 16px, a little under the metadata line; the shadcn Avatar's own border outlines each one. */
const AVATAR_SIZE = "size-4";

function SubagentAvatar({ agent }: { agent: Subagent }) {
  // One letter: two do not fit a 16px circle that overlaps its neighbour, and the name is in the tooltip.
  const letters = initials(agent.name, 1);
  return (
    <Avatar className={AVATAR_SIZE}>
      <AvatarFallback className={cn("text-caption-11-medium", AVATAR_TONE[subagentState(agent.state)])}>
        {/* The name's initial when it has one; the bot glyph when the subagent never said who it is. */}
        {letters || <Bot className="size-3" aria-hidden="true" />}
      </AvatarFallback>
    </Avatar>
  );
}

const CONTROL_RING =
  "rounded-full outline-none focus-visible:ring-2 focus-visible:ring-status-info";

/**
 * A session's subagents as a stack of small avatars, not as rows nested under
 * the card. Each avatar is its own button (open that subagent's work); the
 * overflow chip opens the whole list. They sit beside the card's open button,
 * never inside it, so no control nests another.
 *
 * Names, initials and state come from the subagent itself. There is no image
 * and no vendor to show, so neither is invented.
 */
export function AgentSubagentAvatars({
  subagents,
  max,
  onOpen,
}: {
  subagents: readonly Subagent[];
  /** How many avatars fit; the rest become "+N". */
  max: number;
  onOpen: (subagent: Subagent) => void;
}) {
  const [listOpen, setListOpen] = React.useState(false);
  const ordered = React.useMemo(() => orderSubagents(subagents), [subagents]);
  if (ordered.length === 0) return null;

  const shown = ordered.slice(0, max);
  const extra = ordered.length - shown.length;

  return (
    <AvatarGroup
      role="group"
      aria-label={subagentsLabel(ordered)}
      data-testid="agent-subagent-avatars"
      className="relative z-10 shrink-0 -space-x-1"
    >
      {shown.map((agent) => (
        <Tooltip key={agent.id}>
          <TooltipTrigger asChild>
            <button
              type="button"
              data-testid="agent-subagent-avatar"
              data-state={subagentState(agent.state)}
              aria-label={`Open ${subagentLabel(agent)}`}
              onClick={() => onOpen(agent)}
              className={cn("cursor-pointer", CONTROL_RING)}
            >
              <SubagentAvatar agent={agent} />
            </button>
          </TooltipTrigger>
          <TooltipContent side="top">
            <span className="block">{subagentLabel(agent)}</span>
            {agent.task ? <span className="block max-w-60 truncate text-text-tertiary">{agent.task}</span> : null}
          </TooltipContent>
        </Tooltip>
      ))}

      {extra > 0 ? (
        <Popover open={listOpen} onOpenChange={setListOpen}>
          <Tooltip>
            <TooltipTrigger asChild>
              <PopoverTrigger asChild>
                <button
                  type="button"
                  data-testid="agent-subagent-more"
                  aria-label={`${ordered.length} subagents, show all`}
                  className={cn(
                    "relative flex h-4 min-w-4 cursor-pointer items-center justify-center bg-surface-raised px-1 text-caption-11-medium text-text-secondary",
                    "after:absolute after:inset-0 after:rounded-full after:border after:border-border",
                    CONTROL_RING,
                  )}
                >
                  {`+${extra}`}
                </button>
              </PopoverTrigger>
            </TooltipTrigger>
            <TooltipContent side="top">{`${ordered.length} subagents — show all`}</TooltipContent>
          </Tooltip>
          <PopoverContent align="start" className="w-64 p-1" data-testid="agent-subagent-list">
            <p className="px-2 pt-1.5 pb-1 text-label-11-caps text-text-tertiary">{`Subagents · ${ordered.length}`}</p>
            <ul className="flex max-h-64 flex-col overflow-y-auto">
              {ordered.map((agent) => (
                <li key={agent.id}>
                  <button
                    type="button"
                    data-testid="agent-subagent-row"
                    onClick={() => {
                      setListOpen(false);
                      onOpen(agent);
                    }}
                    className="flex w-full min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left outline-none hover:bg-state-soft focus-visible:ring-2 focus-visible:ring-status-info"
                  >
                    <SubagentAvatar agent={agent} />
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate text-body-13 text-text-primary">{subagentName(agent)}</span>
                      {agent.task ? <span className="truncate text-caption-11 text-text-tertiary">{agent.task}</span> : null}
                    </span>
                    <span className="shrink-0 text-caption-11 text-text-tertiary">
                      {subagentStateWord(agent.state)}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </PopoverContent>
        </Popover>
      ) : null}
    </AvatarGroup>
  );
}
