import { Shimmer } from "@/components/ui/shimmer";
import * as React from "react";
import { ChevronRight } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ThinkingOrb } from "thinking-orbs";

import { cn } from "@/lib/utils";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { FADE_THROUGH } from "@/lib/motion";
import type { TranscriptItem } from "../types";
import type { CheckpointSummary, FileDiff } from "../../../../bindings/devkit/internal/agenthub/models";
import { activityOf, stepSummary, workedFor, workSegments, type Turn } from "../turns";
import {
  AgentPlan,
  AgentSubagent,
  AgentAsyncTask,
  AgentThinking,
  AgentToolCall,
} from "../transcript/index";
import { AgentProse } from "../transcript/AgentMessageRich";
import { MessageActions } from "./MessageActions";
import { AgentTurnChanges } from "./AgentTurnChanges";
import { AgentFileChip } from "../transcript/AgentFileChip";

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
  /**
   * What this turn changed on disk, if it was checkpointed. Absent for a turn
   * that was not — which is not the same as a turn that changed nothing, so the
   * count is simply not drawn rather than drawn as a zero.
   */
  checkpoint?: CheckpointSummary;
  /** Loads one changed file's two versions, when its row is opened. */
  loadFileDiff?: (checkpointId: string, path: string) => Promise<FileDiff>;
  /**
   * Opens the confirmation for taking the session back to this message.
   * `editAfter` is the pencil: the same rewind, and then the message goes back
   * in the composer to be changed and sent again.
   *
   * Offered only on a turn that can be rewound — the button is not a disabled
   * affordance on every message, it is simply absent.
   */
  onRewind?: (promptMessageId: string, editAfter: boolean) => void;
  /** Opens the inspector for a reply that carries cost and timing. */
  onInspect?: (item: TranscriptItem) => void;
  /** Copy, in a message's hover card. */
  onCopy?: (text: string) => void;
  /** Edit, in your own prompt's hover card: its text goes back in the composer. */
  onEditPrompt?: (text: string) => void;
  onOpenFile?: (path: string, line?: number) => void;
};

/**
 * One exchange, drawn the way it reads: your message, the account of what the
 * agent did about it, then its answer.
 *
 * The account reads as prose with evidence, not a log: what the agent said
 * between steps is a sentence on its own ground, always there to read: it was
 * said to you, not to itself. Only the steps that produced it — thinking, tool
 * calls, subagents — run together under one summary per run, "Read files, ran
 * commands, searched the web", closed until asked for. A turn that ran forty
 * tool calls used to put forty cards between a question and its answer, each
 * with its output open, and the answer was the thing you scrolled to find;
 * folding the steps (not the words) keeps that gone without also burying what
 * the agent actually said. The live activity sits after the answer so it
 * stays visible while following a long response.
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
  checkpoint,
  loadFileDiff,
  onRewind,
  onInspect,
  onCopy,
  onEditPrompt,
  onOpenFile,
}: AgentTurnProps) {
  const reduce = useReducedMotion() ?? false;
  // The prompt's own message id, when this turn can be rewound at all. Unlike
  // the file half, a conversation rewind needs no checkpoint — the message id
  // is the whole requirement — so this is offered on any prompt DevKit gave an
  // id to, which since the checkpoint work is every prompt.
  const rewindable = onRewind && turn.prompt?.messageId ? turn.prompt.messageId : "";

  const activity = React.useMemo(() => (live ? activityOf(turn) : null), [live, turn]);
  const answer = React.useMemo(() => turn.answer.filter((item) => item.text?.trim()), [turn.answer]);
  const worked = React.useMemo(() => (live ? "Working" : workedFor(turn)), [live, turn]);
  // Always computed, never gated behind a fold of its own — see the module
  // doc: only the "steps" segments inside this fold themselves (StepGroup).
  const segments = React.useMemo(() => workSegments(turn.work), [turn.work]);
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
      {turn.prompt && (turn.prompt.text?.trim() || turn.prompt.attachments?.length) ? (
        <div className="flex w-full justify-end">
          <MessageActions
            align="end"
            text={turn.prompt.text ?? ""}
            at={turn.prompt.createdAt}
            onCopy={onCopy}
            // The pencil means "edit and run again from here" once a rewind is
            // possible: putting the old text in the composer without taking the
            // conversation back would send it into a session that already
            // answered it.
            onEdit={
              rewindable
                ? () => onRewind?.(rewindable, true)
                : onEditPrompt
            }
            onRewind={rewindable ? () => onRewind?.(rewindable, false) : undefined}
          >
            <div className="flex max-w-[80%] flex-col items-end gap-1.5">
              {/* What the prompt carried besides words, by name: an image-only
                  prompt is still a turn, and the bubble says what it was. */}
              {turn.prompt.attachments?.length ? (
                <div data-testid="turn-prompt-attachments" className="flex max-w-full flex-wrap justify-end gap-1">
                  {turn.prompt.attachments.map((attachment, index) => (
                    <AgentFileChip
                      key={`${attachment.kind}:${attachment.name}:${index}`}
                      path={attachment.name}
                      kind={attachment.kind === "image" ? "image" : "file"}
                    />
                  ))}
                </div>
              ) : null}
              {turn.prompt.text?.trim() ? (
                <p
                  data-testid="turn-prompt"
                  // wrap-anywhere, not break-words: a pasted token or URL with
                  // no spaces is still "unbreakable" to the browser's
                  // min-content calculation under break-words (the CSS spec
                  // excludes it there on purpose) — an ancestor sizing itself
                  // off that min-content still forces the bubble open
                  // sideways even though break-words looks like it should
                  // have wrapped it. wrap-anywhere counts toward min-content,
                  // so the bubble's own max-w-[80%] actually holds.
                  className="rounded-lg bg-surface-tinted px-3 py-2 text-body-14 whitespace-pre-wrap wrap-anywhere text-text-primary select-text"
                >
                  {turn.prompt.text}
                </p>
              ) : null}
            </div>
          </MessageActions>
        </div>
      ) : null}

      {turn.work.length > 0 ? (
        <div data-testid="turn-work" className="flex w-full flex-col gap-3">
          <div className="w-full border-b border-border-subtle pb-2 text-body-13 text-text-tertiary">
            <SwapText text={worked} animate={latest} reduce={reduce} active={false} />
          </div>
          {segments.map((segment) => {
            if (segment.kind === "text") {
              // What the agent said between steps was said to you, not to
              // itself: it stands on its own ground like the answer, never
              // behind the steps' fold.
              return <AgentProse key={segment.item.id} text={segment.item.text ?? ""} />;
            }
            if (segment.kind === "plan") {
              return <AgentPlan key={segment.item.id} entries={segment.item.plan ?? []} />;
            }
            return <StepGroup key={segment.id} items={segment.items} onOpenFile={onOpenFile} />;
          })}
        </div>
      ) : null}

      {answer.map((item) => (
        <MessageActions
          key={item.id}
          align="start"
          text={item.text ?? ""}
          at={item.createdAt}
          meta={turnMeta(item)}
          onDetails={onInspect ? () => onInspect(item) : undefined}
          onCopy={onCopy}
        >
          <div className="w-full">
            <AgentProse
              text={item.text ?? ""}
              // Only while the turn is live, and only on mount: a block of the
              // answer arrives once, and the text streaming into it afterwards is
              // already motion enough. A loaded history renders standing still.
              wrapBlock={wrapAnswerBlock}
            />
          </div>
        </MessageActions>
      ))}

      {/* What the project looks like after, beside what the agent said it did.
          Last, not up front with the work: the fold above is the account of
          the work and this is its result, but the result of the answer you
          just read, so it reads after that answer rather than before it. */}
      {checkpoint ? (
        <AgentTurnChanges checkpoint={checkpoint} loadDiff={loadFileDiff} />
      ) : null}

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
 * Closed by default: this is the only fold a run of thinking, tool calls or
 * subagents sits behind now (see AgentTurn — the words around it are never
 * folded), so nothing above it is hiding the steps for it. A long run scrolls
 * inside a 240px well (max-h-60) and fades at the foot, so one directory crawl
 * cannot push the answer a screen down.
 */
const LONG_RUN = 8;

export function StepGroup({
  items,
  onOpenFile,
  defaultOpen = false,
}: {
  items: TranscriptItem[];
  onOpenFile?: (path: string, line?: number) => void;
  /** Lets a caller (a unit test, mainly) start it expanded. */
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = React.useState(defaultOpen);
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
    case "async_task_spawned":
    case "async_task_progress":
    case "async_task_state_update":
      return item.asyncTask ? <AgentAsyncTask variant="row" task={item.asyncTask} /> : null;
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
