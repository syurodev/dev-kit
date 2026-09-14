import * as React from "react";
import { ChevronRight } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ThinkingOrb } from "thinking-orbs";

import { cn } from "@/lib/utils";
import { FADE_THROUGH } from "@/lib/motion";
import type { TranscriptItem, Usage } from "../types";
import { activityOf, workedFor, type Turn } from "../turns";
import {
  AgentInvokeResult,
  AgentPlan,
  AgentRateLimit,
  AgentSubagent,
  AgentThinking,
  AgentToolCall,
} from "../transcript/index";
import { AgentProse } from "../transcript/AgentMessageRich";

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
};

/**
 * One exchange, drawn the way it reads: your message, one line for everything
 * the agent did about it, then its answer.
 *
 * The work is folded by default. A turn that runs forty tool calls used to put
 * forty cards between a question and its answer, each with its output open,
 * and the answer was the thing you scrolled to find. Folded, the line still
 * says what is happening while it happens — the tool's own title beside an orb
 * whose motion matches the kind of step — and what it came to once it is done.
 *
 * No bubble and no name on the agent's side, and no avatar on either. With one
 * person and one agent in the room, the side of the column already says who is
 * speaking; a bubble around a document squeezes headings and code into a chat
 * line.
 */
export function AgentTurn({ turn, live = false, latest = false, vendor, onInspect }: AgentTurnProps) {
  const reduce = useReducedMotion() ?? false;
  const [open, setOpen] = React.useState(false);

  const activity = live ? activityOf(turn) : null;
  const answer = turn.answer.filter((item) => item.text?.trim());
  const last = answer[answer.length - 1];
  const meta = last ? turnMeta(last) : undefined;
  // Reduce Motion keeps the fade and drops the travel, scale and blur — the
  // parts accessibility.md › Motion names, "zooming, scaling, and peripheral
  // motion". Text still arrives; it just stops moving to get there.
  const from = reduce ? { opacity: 0 } : FADE_THROUGH.from;

  return (
    <section data-testid="agent-turn" data-live={live || undefined} className="flex w-full flex-col gap-5">
      {turn.prompt?.text?.trim() ? (
        <div className="flex w-full justify-end">
          <p
            data-testid="turn-prompt"
            className="max-w-[80%] rounded-2xl bg-surface-tinted px-4 py-2.5 text-body-16 whitespace-pre-wrap break-words text-text-primary select-text"
          >
            {turn.prompt.text}
          </p>
        </div>
      ) : null}

      {live || turn.work.length > 0 ? (
        <div data-testid="turn-work" data-state={open ? "expanded" : "collapsed"} className="flex w-full flex-col">
          <button
            type="button"
            onClick={() => setOpen((value) => !value)}
            aria-expanded={open}
            className="flex w-full min-w-0 cursor-pointer items-center gap-2 border-b border-border-subtle pb-2 text-left text-body-13 text-text-tertiary hover:text-text-secondary"
          >
            {activity ? (
              <ThinkingOrb state={activity.orb} size={20} aria-label={activity.label} className="shrink-0" />
            ) : null}
            <SwapText text={activity ? activity.label : workedFor(turn)} animate={latest} reduce={reduce} />
            <ChevronRight
              aria-hidden="true"
              className={cn("size-3.5 shrink-0 transition-transform duration-(--motion-fast)", open && "rotate-90")}
            />
          </button>
          {open ? (
            <div className="flex w-full flex-col gap-2 pt-3">
              {turn.work.map((item) => (
                <WorkRow key={item.id} item={item} />
              ))}
            </div>
          ) : null}
        </div>
      ) : null}

      {answer.map((item) => (
        <AgentProse
          key={item.id}
          text={item.text ?? ""}
          // Only while the turn is live, and only on mount: a block of the
          // answer arrives once, and the text streaming into it afterwards is
          // already motion enough. A loaded history renders standing still.
          wrapBlock={(block) => (
            <motion.div
              initial={live ? from : false}
              animate={FADE_THROUGH.to}
              transition={FADE_THROUGH.enter}
            >
              {block}
            </motion.div>
          )}
        />
      ))}

      {meta && last && onInspect ? (
        <button
          type="button"
          data-testid="message-open-meta"
          onClick={() => onInspect(last)}
          className="cursor-pointer self-start text-caption-11 text-text-tertiary hover:text-text-primary"
        >
          {meta}
        </button>
      ) : null}

      {turn.notes.map((item) => (
        <Note key={item.id} item={item} vendor={vendor} />
      ))}
    </section>
  );
}

/**
 * The line of a work disclosure, swapping one sentence for the next with
 * fade-through.
 *
 * Only the newest turn animates, and not on first mount (`initial={false}`):
 * opening a session replays nothing.
 */
function SwapText({ text, animate, reduce }: { text: string; animate: boolean; reduce: boolean }) {
  if (!animate) return <span className="min-w-0 truncate">{text}</span>;
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
          {text}
        </motion.span>
      </AnimatePresence>
    </span>
  );
}

/** One step of the work, in the order the agent took it. */
function WorkRow({ item }: { item: TranscriptItem }) {
  switch (String(item.kind)) {
    case "agent_thought_chunk": {
      const raw = item.raw as Record<string, unknown> | undefined;
      return (
        <AgentThinking
          text={item.text ?? ""}
          duration={typeof raw?.duration === "string" ? raw.duration : undefined}
        />
      );
    }
    case "agent_message_chunk":
      // What the agent said between steps. Quieter than the answer: it is
      // narration of the work, and it is inside the work.
      return item.text?.trim() ? (
        <p className="text-body-13 whitespace-pre-wrap break-words text-text-secondary select-text">
          {item.text}
        </p>
      ) : null;
    case "plan":
      return <AgentPlan entries={item.plan ?? []} />;
    case "subagent_spawned":
    case "subagent_state_update":
      return item.subagent ? <AgentSubagent subagent={item.subagent} /> : null;
    default:
      return (
        <AgentToolCall
          call={item.toolCall ?? { id: item.toolCallId || item.id, title: "Tool execution", status: "completed" }}
        />
      );
  }
}

/** Rows that are neither work nor words: a rate limit, an invoke result. */
function Note({ item, vendor }: { item: TranscriptItem; vendor?: string }) {
  const raw = rawOf(item);
  switch (String(item.kind)) {
    case "rate_limit":
      return (
        <AgentRateLimit
          level={raw?.level === "exhausted" ? "exhausted" : "warning"}
          message={item.text}
          resetTime={typeof raw?.resetTime === "string" ? raw.resetTime : undefined}
        />
      );
    case "invoke_result":
      return (
        <AgentInvokeResult
          agentName={typeof raw?.agentName === "string" ? raw.agentName : vendor || "Agent"}
          stopReason={typeof raw?.stopReason === "string" ? raw.stopReason : undefined}
          duration={typeof raw?.duration === "string" ? raw.duration : undefined}
          cost={typeof raw?.cost === "object" && raw.cost !== null ? (raw.cost as Partial<Usage>) : undefined}
          summary={item.text || (typeof raw?.summary === "string" ? raw.summary : "")}
          filesChanged={Array.isArray(raw?.filesChanged) ? (raw.filesChanged as string[]) : []}
        />
      );
    default:
      return null;
  }
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
  if (!raw) return undefined;
  const cost = typeof raw.cost === "number" ? raw.cost : undefined;
  const parts = [
    typeof raw.duration === "string" ? raw.duration : undefined,
    cost !== undefined ? `$${cost.toFixed(cost < 0.01 ? 4 : 2)}` : undefined,
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : undefined;
}
