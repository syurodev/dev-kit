import { Shimmer } from "@/components/ui/shimmer";
import * as React from "react";
import { ChevronRight } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ThinkingOrb } from "thinking-orbs";

import { cn } from "@/lib/utils";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { FADE_THROUGH } from "@/lib/motion";
import type { TranscriptItem } from "../types";
import { activityOf, stepSummary, workedFor, workSegments, type Turn } from "../turns";
import {
  AgentPlan,
  AgentSubagent,
  AgentThinking,
  AgentToolCall,
} from "../transcript/index";
import { AgentProse } from "../transcript/AgentMessageRich";
import { MessageHoverCard } from "./MessageHoverCard";

export type AgentTurnProps = {
  turn: Turn;
  /** The agent is still on this turn: the orb runs and the line follows its steps. */
  live?: boolean;
  /**
   * This is the newest turn. Its line animates when it changes — including the
   * last change, from the step it was on to "Worked for …" — and older turns
   * stand still, so scrolling back through history never replays anything.
   */
  latest?: boolean;
  vendor?: string;
  /** Opens the inspector for a reply that carries cost and timing. */
  onInspect?: (item: TranscriptItem) => void;
  /** Copy, in a message's hover card. */
  onCopy?: (text: string) => void;
  /** Edit, in your own prompt's hover card: its text goes back in the composer. */
  onEditPrompt?: (text: string) => void;
  onOpenFile?: (path: string, line?: number) => void;
};

/**
 * One exchange, drawn the way it reads: your message, one line for everything
 * the agent did about it, then its answer.
 *
 * The work is folded by default. A turn that runs forty tool calls used to put
 * forty cards between a question and its answer, each with its output open,
 * and the answer was the thing you scrolled to find. Folded, the line still
 * summarises the work and what it came to once done. The live activity sits
 * after the answer so it stays visible while following a long response.
 *
 * Opened, the work reads as an account rather than a log: what the agent said
 * as prose, and each run of steps between two sentences under one summary —
 * "Read files, ran commands, searched the web" — with the steps as plain rows.
 *
 * No bubble and no name on the agent's side, and no avatar on either. With one
 * person and one agent in the room, the side of the column already says who is
 * speaking; a bubble around a document squeezes headings and code into a chat
 * line.
 *
 * Spacing is the scale's named steps and nothing between them: 16 (group)
 * between the parts of a turn, 12 (control) between the parts of its work, 8
 * (inline) inside a line. Work lines are 13px beside the answer's 14: the
 * account of what was done is the quieter register of the two.
 */
export const AgentTurn = React.memo(function AgentTurn({
  turn,
  live = false,
  latest = false,
  vendor,
  onInspect,
  onCopy,
  onEditPrompt,
  onOpenFile,
}: AgentTurnProps) {
  const reduce = useReducedMotion() ?? false;
  const [open, setOpen] = React.useState(false);

  const activity = React.useMemo(() => (live ? activityOf(turn) : null), [live, turn]);
  const answer = React.useMemo(() => turn.answer.filter((item) => item.text?.trim()), [turn.answer]);
  const worked = React.useMemo(() => (live ? "Working" : workedFor(turn)), [live, turn]);
  const segments = React.useMemo(() => (open ? workSegments(turn.work) : []), [open, turn.work]);
  // Reduce Motion keeps the fade and drops the travel and scale — the
  // parts accessibility.md › Motion names, "zooming, scaling, and peripheral
  // motion". Text still arrives; it just stops moving to get there.
  const from = React.useMemo(() => (reduce ? { opacity: 0 } : FADE_THROUGH.from), [reduce]);
  const wrapAnswerBlock = React.useCallback(
    (block: React.ReactNode) => (
      <motion.div initial={live ? from : false} animate={FADE_THROUGH.to} transition={FADE_THROUGH.enter}>
        {block}
      </motion.div>
    ),
    [from, live],
  );

  return (
    <section
      data-testid="agent-turn"
      data-turn-id={turn.id}
      data-live={live || undefined}
      className="flex w-full flex-col gap-4"
      // A jump from the transcript's ticks lands the prompt clear of the glass header.
      style={{ scrollMarginTop: "calc(var(--header-inset, 0px) + var(--space-section))" }}
    >
      {turn.prompt?.text?.trim() ? (
        <div className="flex w-full justify-end">
          <MessageHoverCard
            align="end"
            text={turn.prompt.text}
            at={turn.prompt.createdAt}
            onCopy={onCopy}
            onEdit={onEditPrompt}
          >
            <p
              data-testid="turn-prompt"
              className="max-w-[80%] rounded-lg bg-surface-tinted px-3 py-2 text-body-14 whitespace-pre-wrap break-words text-text-primary select-text"
            >
              {turn.prompt.text}
            </p>
          </MessageHoverCard>
        </div>
      ) : null}

      {turn.work.length > 0 ? (
        <div data-testid="turn-work" data-state={open ? "expanded" : "collapsed"} className="flex w-full flex-col">
          <button
            type="button"
            onClick={() => setOpen((value) => !value)}
            aria-expanded={open}
            className="flex w-full min-w-0 cursor-pointer items-center gap-2 border-b border-border-subtle pb-2 text-left text-body-13 text-text-tertiary hover:text-text-secondary"
          >
            <SwapText text={worked} animate={latest} reduce={reduce} active={false} />
            <ChevronRight
              aria-hidden="true"
              className={cn("size-3.5 shrink-0 transition-transform duration-(--motion-fast)", open && "rotate-90")}
            />
          </button>
          <AnimatedCollapse open={open} className="flex w-full flex-col gap-3 pt-3">
              {segments.map((segment) => {
                if (segment.kind === "text") {
                  // What the agent said between steps is prose like its answer:
                  // it is the account of the work, and the steps are its evidence.
                  return <AgentProse key={segment.item.id} text={segment.item.text ?? ""} />;
                }
                if (segment.kind === "plan") {
                  return <AgentPlan key={segment.item.id} entries={segment.item.plan ?? []} />;
                }
                return <StepGroup key={segment.id} items={segment.items} onOpenFile={onOpenFile} />;
              })}
          </AnimatedCollapse>
        </div>
      ) : null}

      {answer.map((item) => (
        <MessageHoverCard
          key={item.id}
          align="start"
          text={item.text ?? ""}
          at={item.createdAt}
          meta={turnMeta(item)}
          onDetails={onInspect ? () => onInspect(item) : undefined}
          onCopy={onCopy}
        >
          {/* AgentProse is not a DOM element; the trigger needs one to measure. */}
          <div className="w-full">
            <AgentProse
              text={item.text ?? ""}
              // Only while the turn is live, and only on mount: a block of the
              // answer arrives once, and the text streaming into it afterwards is
              // already motion enough. A loaded history renders standing still.
              wrapBlock={wrapAnswerBlock}
            />
          </div>
        </MessageHoverCard>
      ))}

      {activity ? (
        <div
          data-testid="turn-activity"
          role="status"
          className="flex w-full min-w-0 items-center gap-2 text-body-13 text-text-tertiary"
        >
          <ThinkingOrb
            state={activity.orb}
            size={20}
            paused={reduce}
            aria-hidden="true"
            className="shrink-0"
          />
          <SwapText text={activity.label} animate={latest} reduce={reduce} active />
        </div>
      ) : null}
    </section>
  );
});

/**
 * A run of steps between two things the agent said, under one line saying what
 * kind of work it was.
 *
 * Open by default: the turn's own fold is where the work is hidden, and a
 * second fold inside it would make opening "Worked for" show a list of closed
 * lists. A long run scrolls inside a 240px well (max-h-60) and fades at the
 * foot, so one directory crawl cannot push the answer a screen down.
 */
const LONG_RUN = 8;

export function StepGroup({ items, onOpenFile }: { items: TranscriptItem[]; onOpenFile?: (path: string, line?: number) => void }) {
  const [open, setOpen] = React.useState(true);
  const long = items.length > LONG_RUN;
  // One step needs no summary: "Thought" over a row reading "Thought" says the
  // same thing twice, and a fold around a single line hides nothing.
  if (items.length === 1) return <StepRow item={items[0]} onOpenFile={onOpenFile} />;
  return (
    <div data-testid="step-group" data-state={open ? "expanded" : "collapsed"} className="flex w-full flex-col">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        className="flex w-full min-w-0 cursor-pointer items-center gap-2 text-left text-body-13 text-text-tertiary hover:text-text-secondary"
      >
        <span className="min-w-0 truncate">{stepSummary(items)}</span>
        <ChevronRight
          aria-hidden="true"
          className={cn("size-3.5 shrink-0 transition-transform duration-(--motion-fast)", open && "rotate-90")}
        />
      </button>
      <AnimatedCollapse open={open} className="pt-2">
        <div
          className={cn("w-full", long && "max-h-60 overflow-y-auto")}
          // The fade covers the last 32px (page step) of the well, and the list
          // carries the same padding at its foot, so the final row can always
          // be scrolled clear of it.
          style={
            long
              ? {
                  maskImage: "linear-gradient(to bottom, black calc(100% - var(--space-page)), transparent)",
                  WebkitMaskImage: "linear-gradient(to bottom, black calc(100% - var(--space-page)), transparent)",
                }
              : undefined
          }
        >
          <div className={cn("flex w-full flex-col gap-2", long && "pb-8")}>
            {items.map((item) => (
              <StepRow key={item.id} item={item} onOpenFile={onOpenFile} />
            ))}
          </div>
        </div>
      </AnimatedCollapse>
    </div>
  );
}

/** One step, as a row. */
function StepRow({ item, onOpenFile }: { item: TranscriptItem; onOpenFile?: (path: string, line?: number) => void }) {
  switch (String(item.kind)) {
    case "agent_thought_chunk": {
      const raw = item.raw as Record<string, unknown> | undefined;
      return (
        <AgentThinking
          variant="row"
          text={item.text ?? ""}
          duration={typeof raw?.duration === "string" ? raw.duration : undefined}
        />
      );
    }
    case "subagent_spawned":
    case "subagent_state_update":
      return item.subagent ? <AgentSubagent variant="row" subagent={item.subagent} /> : null;
    default:
      return (
        <AgentToolCall
          variant="row"
          sessionId={item.sessionId}
          onOpenFile={onOpenFile}
          call={item.toolCall ?? { id: item.toolCallId || item.id, title: "Tool execution", status: "completed" }}
        />
      );
  }
}

/**
 * The line of a work disclosure, swapping one sentence for the next with
 * fade-through.
 *
 * Only the newest turn animates, and not on first mount (`initial={false}`):
 * opening a session replays nothing.
 */
function SwapText({ text, animate, reduce, active }: { text: string; animate: boolean; reduce: boolean; active: boolean }) {
  if (!animate) return <Shimmer active={active} className="min-w-0 truncate">{text}</Shimmer>;
  return (
    <span className="flex min-w-0">
      <AnimatePresence mode="wait" initial={false}>
        <motion.span
          key={text}
          className="block min-w-0 truncate"
          initial={reduce ? { opacity: 0 } : FADE_THROUGH.from}
          animate={{ ...FADE_THROUGH.to, transition: { ...FADE_THROUGH.enter, delay: FADE_THROUGH.delay } }}
          exit={
            reduce
              ? { opacity: 0, transition: FADE_THROUGH.exit }
              : { ...FADE_THROUGH.out, transition: FADE_THROUGH.exit }
          }
        >
          <Shimmer active={active} className="block truncate">{text}</Shimmer>
        </motion.span>
      </AnimatePresence>
    </span>
  );
}

/** The raw envelope, when there is one to read. */
function rawOf(item: TranscriptItem): Record<string, unknown> | undefined {
  return typeof item.raw === "object" && item.raw !== null ? (item.raw as Record<string, unknown>) : undefined;
}

/**
 * "1.2s · $0.03" — the affordance that opens the inspector. Nothing is rendered
 * when the reply carried no numbers, so there is never a button with no label.
 */
function turnMeta(item: TranscriptItem): string | undefined {
  const raw = rawOf(item);
  const amount = typeof raw?.amount === "number" ? raw.amount : undefined;
  return amount === undefined ? undefined : `$${amount.toFixed(amount < 0.01 ? 4 : 2)}`;
}
