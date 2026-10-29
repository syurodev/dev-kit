import { FileIcon } from "@/components/file-icon/FileIcon";
import { Shimmer } from "@/components/ui/shimmer";
import * as React from "react";
import { ChevronRight } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ThinkingOrb } from "thinking-orbs";

import { cn } from "@/lib/utils";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { FADE_THROUGH } from "@/lib/motion";
import type { PromptAttachment, TranscriptItem } from "../types";
import type { CheckpointSummary, FileDiff } from "../../../../bindings/devkit/internal/agenthub/models";
import { activityOf, stepCount, stepSummary, workedFor, workSegments, type Turn } from "../turns";
import {
  AgentPlan,
  AgentSubagent,
  AgentAsyncTask,
  AgentThinking,
  AgentToolCall,
} from "../transcript/index";
import { NO_RESULTS, surfaceResults, ToolResultCard, type StepResults } from "../transcript/ToolResultCard";
import { AgentToolActivity, isToolActivity } from "../transcript/AgentToolActivity";
import { AgentRunningCommand, isRunningCommand } from "../transcript/AgentRunningCommand";
import { cannotFinish, useSettleFacts } from "../transcript/toolSettle";
import { AgentProse } from "../transcript/AgentMessageRich";
import { MessageActions } from "./MessageActions";
import { PeerMessage } from "../transcript/PeerMessage";
import { AccountNoticeLine } from "../transcript/AccountNotice";
import { AgentTurnChanges } from "./AgentTurnChanges";

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
   * This turn holds the session's newest plan, the one the docked summary
   * card shows in full. Then its card gives way to a single quiet line while
   * the summary is docked; older turns' plans are untouched.
   */
  latestPlan?: boolean;
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
  latestPlan = false,
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
  // Which results stand outside the folds: a few per turn, in call order,
  // shared out among its runs of steps.
  const stepResults = React.useMemo(() => {
    const runs = segments.filter((segment) => segment.kind === "steps");
    const shares = surfaceResults(runs.map((run) => run.items));
    return new Map(runs.map((run, index) => [run.id, shares[index]]));
  }, [segments]);
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
      {turn.prompt?.source ? (
        // Another session's (or the terminal's) words are not the person's: no
        // bubble on the right, no edit or rewind — those act on what you said.
        <div className="flex w-full justify-start">
          <PeerMessage source={turn.prompt.source} text={turn.prompt.text ?? ""} at={turn.prompt.createdAt} onCopy={onCopy} />
        </div>
      ) : turn.prompt && (turn.prompt.text?.trim() || turn.prompt.attachments?.length) ? (
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
            <div className="flex max-w-4/5 flex-col items-end gap-1.5">
              {/* What the prompt carried besides words, by name: an image-only
                  prompt is still a turn, and the bubble says what it was. */}
              {turn.prompt.attachments?.length ? (
                <div data-testid="turn-prompt-attachments" className="flex max-w-full flex-wrap justify-end gap-2">
                  {turn.prompt.attachments.map((attachment, index) => (
                    <div
                      key={`${attachment.kind}:${attachment.name}:${index}`}
                      data-testid="turn-attachment-card"
                      className="flex max-w-full items-center gap-2.5 rounded-xl bg-surface-tinted py-2 pr-3.5 pl-2"
                    >
                      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-surface-base">
                        <FileIcon path={attachment.name} className="size-4" />
                      </span>
                      <span className="flex min-w-0 flex-col">
                        <span className="max-w-60 truncate text-body-13 text-text-primary">{attachment.name}</span>
                        <span className="text-body-12 text-text-tertiary">{attachmentDetail(attachment)}</span>
                      </span>
                    </div>
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
                  // so the bubble's own max-w-4/5 actually holds.
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
          <div className="flex w-full items-center gap-1 border-b border-border-subtle pb-2 text-body-13 text-text-tertiary">
            <SwapText text={worked} animate={latest} reduce={reduce} active={false} />
            <ChevronRight className="size-3.5 shrink-0" aria-hidden="true" />
          </div>
          {segments.map((segment) => {
            if (segment.kind === "text") {
              // What the agent said between steps was said to you, not to
              // itself: it stands on its own ground like the answer, never
              // behind the steps' fold.
              return <AgentProse key={segment.item.id} text={segment.item.text ?? ""} settled={!live} />;
            }
            if (segment.kind === "plan") {
              const entries = segment.item.plan ?? [];
              // An empty plan has nothing to say in the summary: keep its card.
              if (!latestPlan || entries.length === 0) return <AgentPlan key={segment.item.id} entries={entries} />;
              const done = entries.filter((e) => e.status === "completed").length;
              return (
                <React.Fragment key={segment.item.id}>
                  <AgentPlan entries={entries} className="@6xl/agent-conversation:hidden" />
                  <p
                    data-testid="turn-plan-docked"
                    className="hidden w-full items-center gap-2 text-caption-11 text-text-tertiary @6xl/agent-conversation:flex"
                  >
                    {`Plan · ${done} of ${entries.length} — in the summary`}
                  </p>
                </React.Fragment>
              );
            }
            return (
              <StepGroup
                key={segment.id}
                items={segment.items}
                subagents={turn.subagents}
                onOpenFile={onOpenFile}
                results={stepResults.get(segment.id) ?? NO_RESULTS}
              />
            );
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
              // Symbols and commit hashes become references once the turn is over.
              settled={!live}
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

      {/* A session that moved to another account does it before the next prompt,
          so the line closes the turn it follows and reads as sitting between the two. */}
      {turn.notes.map((note) =>
        note.accountNotice ? <AccountNoticeLine key={note.id} notice={note.accountNotice} vendor={vendor ?? ""} /> : null,
      )}

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
 * folded), so nothing above it is hiding the steps for it. Opened, it shows
 * every step at full height: opening it is the request to read them, and a
 * scrolling well inside the transcript's own scroller hid most of a long run.
 *
 * The steps that changed something in DevKit — a board or task made, a commit,
 * a push, a request sent (workbench/toolActivity.ts) — are not left behind the
 * fold: each stays on screen as a card under the run's line, running while it
 * runs. Reading forty files is the account; making a board is news. The fold
 * still holds every step, those included, for their raw input and output.
 */

export type ActivityById = Readonly<Record<string, TranscriptItem[]>>;

/**
 * Which of a run's fixed slots draw — head (its row alone, or the summary
 * line), fold, results, activities — always in that order, so the results
 * slot is the same element whether the run has one step or more: a lone
 * card stays mounted when a second step joins it.
 */
export function stepRunLayout({
  items,
  shown,
  hidden,
  activities,
  commands,
}: {
  items: readonly TranscriptItem[];
  shown: readonly TranscriptItem[];
  hidden: readonly TranscriptItem[];
  activities: number;
  commands: number;
}): { lone: boolean; head: "none" | "row" | "summary"; fold: boolean; results: boolean; activities: boolean } {
  // One step needs no summary: "Thought" over a row reading "Thought" says the
  // same thing twice, and a fold around a single line hides nothing. Unless it
  // is a card: then the line is the way to its raw call. A lone result is its
  // card, no row.
  const lone = items.length === 1 && activities === 0 && commands === 0;
  return {
    lone,
    head: lone ? (shown.length === 1 ? "none" : "row") : "summary",
    fold: !lone,
    // Results left in the fold are still said, even when the turn's cards are spent.
    results: shown.length > 0 || hidden.length > 0,
    activities: activities > 0 || commands > 0,
  };
}

export function StepGroup({
  items,
  subagents,
  onOpenFile,
  defaultOpen = false,
  results: given,
}: {
  items: TranscriptItem[];
  /** What each subagent in `items` did, by id — see Turn.subagents. */
  subagents?: ActivityById;
  onOpenFile?: (path: string, line?: number) => void;
  /** Lets a caller (a unit test, mainly) start it expanded. */
  defaultOpen?: boolean;
  /**
   * This run's share of the turn's results (surfaceResults: the cap is per
   * turn). Left out, the run is taken as a turn of its own. NO_RESULTS inside
   * a subagent: its work is its detail, and the lead agent reports what
   * matters from it.
   */
  results?: StepResults;
}) {
  const [open, setOpen] = React.useState(defaultOpen);
  // The step the "+N more" line points at, focused once the fold opens on it;
  // `n` counts the clicks, so asking again focuses it again.
  const [focus, setFocus] = React.useState<{ id: string; n: number } | null>(null);
  const runRef = React.useRef<HTMLDivElement>(null);
  const activities = React.useMemo(() => items.filter((item) => isToolActivity(item.toolCall)), [items]);
  // A shell command still running: it gets a card with its clock while it
  // runs (AgentRunningCommand), so a ten-minute build is not one quiet row
  // behind the fold.
  // Not one that was interrupted: no clock for a command nothing can finish.
  const settle = useSettleFacts();
  const commands = React.useMemo(
    () => items.filter((item) => isRunningCommand(item) && !cannotFinish({ ...settle, subagent: Boolean(item.origin) })),
    [items, settle.earlierTurn, settle.sessionQuiet],
  );
  // A result the owning module draws (a query's rows, a plan, a message) is
  // what the person asked for, not the account of getting it: it stands after
  // the fold, in call order (ToolResultCard.tsx).
  const own = React.useMemo(() => (given ? null : surfaceResults([items])[0]), [given, items]);
  const { shown, hidden } = given ?? own ?? NO_RESULTS;
  // Steps whose view already stands after the fold, as a result or activity
  // card: inside the fold they stay one-line rows (their raw output, not the card again).
  const outside = React.useMemo(
    () => new Set([...shown, ...activities].map((item) => item.id)),
    [shown, activities],
  );

  React.useEffect(() => {
    if (!open || !focus) return;
    // Compared, not put in a selector: an id is the vendor's string.
    const rows = runRef.current?.querySelectorAll<HTMLElement>("[data-step-id]") ?? [];
    const row = Array.from(rows).find((element) => element.dataset.stepId === focus.id);
    row
      ?.querySelector<HTMLElement>('button, [href], [tabindex]:not([tabindex="-1"])')
      ?.focus({ preventScroll: true });
  }, [open, focus]);

  const layout = stepRunLayout({ items, shown, hidden, activities: activities.length, commands: commands.length });
  const { lone } = layout;
  const loneCard = lone && layout.head === "none";
  // One wrapper and fixed slots (stepRunLayout). Hidden when the only thing in
  // it is a step that draws nothing — a subagent or task with nothing to show.
  return (
    <div
      ref={runRef}
      data-step-run=""
      data-testid={lone ? undefined : "step-group"}
      data-state={lone ? undefined : open ? "expanded" : "collapsed"}
      className="flex w-full min-w-0 flex-col empty:hidden"
    >
      {layout.head === "none" ? null : layout.head === "row" ? (
        <StepRow item={items[0]} subagents={subagents} onOpenFile={onOpenFile} />
      ) : (
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
      )}
      {layout.fold ? (
        <AnimatedCollapse open={open} className="pt-2">
          <div className="flex w-full flex-col gap-2">
            {items.map((item) => (
              <StepRow
                key={item.id}
                item={item}
                subagents={subagents}
                onOpenFile={onOpenFile}
                resultOutside={outside.has(item.id)}
              />
            ))}
          </div>
        </AnimatedCollapse>
      ) : null}
      {layout.results ? (
        <div
          data-testid="step-results"
          className={cn("flex w-full min-w-0 flex-col gap-2 empty:hidden", !lone && "pt-2")}
        >
          {shown.map((item) => (
            <ToolResultCard
              key={item.id}
              call={item.toolCall!}
              sessionId={item.sessionId}
              origin={item.origin}
              asRow={loneCard}
              onOpenFile={onOpenFile}
            />
          ))}
          {hidden.length > 0 ? (
            <button
              type="button"
              data-testid="step-results-more"
              onClick={() => {
                setOpen(true);
                setFocus((last) => ({ id: hidden[0].id, n: (last?.n ?? 0) + 1 }));
              }}
              className={cn(
                "self-start cursor-pointer text-caption-11 text-text-tertiary underline decoration-border-subtle hover:text-text-secondary",
                "focus-visible:ring-ring focus-visible:ring-1 focus-visible:outline-none",
              )}
            >
              {`+${hidden.length} more result${hidden.length === 1 ? "" : "s"} in the work above`}
            </button>
          ) : null}
        </div>
      ) : null}
      {layout.activities ? (
        <div data-testid="step-activities" className="flex w-full min-w-0 flex-col gap-2 pt-2 empty:hidden">
          {activities.map((item) => (
            <AgentToolActivity key={item.id} call={item.toolCall!} sessionId={item.sessionId} origin={item.origin} />
          ))}
          {commands.map((item) => (
            <AgentRunningCommand key={item.id} item={item} />
          ))}
        </div>
      ) : null}
    </div>
  );
}

/** One step, as a row. */
function StepRow({
  item,
  subagents,
  onOpenFile,
  resultOutside,
}: {
  item: TranscriptItem;
  subagents?: ActivityById;
  onOpenFile?: (path: string, line?: number) => void;
  /** Its result already stands as a card after the fold (AgentToolCall.resultOutside). */
  resultOutside?: boolean;
}) {
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
    case "subagent_state_update": {
      if (!item.subagent) return null;
      const activity = subagents?.[item.subagent.id];
      if (!activity?.length) return <AgentSubagent variant="row" subagent={item.subagent} />;
      return (
        <SubagentStep subagent={item.subagent} activity={activity} subagents={subagents} onOpenFile={onOpenFile} />
      );
    }
    case "async_task_spawned":
    case "async_task_progress":
    case "async_task_state_update":
      return item.asyncTask ? <AgentAsyncTask variant="row" task={item.asyncTask} /> : null;
    default:
      return (
        <AgentToolCall
          variant="row"
          sessionId={item.sessionId}
          stepId={item.id}
          origin={item.origin}
          resultOutside={resultOutside}
          onOpenFile={onOpenFile}
          call={item.toolCall ?? { id: item.toolCallId || item.id, title: "Tool execution", status: "completed" }}
        />
      );
  }
}

/**
 * A subagent and, when opened, everything it did: the brief it was given, what
 * it said along the way, its steps folded the way the turn's own are, and the
 * report it came back with.
 *
 * Nested rather than in the turn's own flow: its rows arrive interleaved with
 * the lead agent's (see turns.ts splitFlow), and read in arrival order a pair
 * of parallel subagents turned one account into three talking over each
 * other. The lead agent reads the report and says what matters in its own
 * words, so the report is the subagent's detail, not the answer.
 */
function SubagentStep({
  subagent,
  activity,
  subagents,
  onOpenFile,
}: {
  subagent: NonNullable<TranscriptItem["subagent"]>;
  activity: TranscriptItem[];
  subagents?: ActivityById;
  onOpenFile?: (path: string, line?: number) => void;
}) {
  const [open, setOpen] = React.useState(false);
  const steps = React.useMemo(() => stepCount(activity), [activity]);
  return (
    <div data-testid="subagent-step" data-state={open ? "expanded" : "collapsed"} className="flex w-full min-w-0 flex-col">
      <AgentSubagent
        variant="row"
        subagent={subagent}
        steps={steps}
        expanded={open}
        onToggle={() => setOpen((value) => !value)}
      />
      <AnimatedCollapse open={open} className="pt-2">
        <SubagentActivity subagent={subagent} activity={activity} subagents={subagents} onOpenFile={onOpenFile} />
      </AnimatedCollapse>
    </div>
  );
}

/**
 * What a subagent did, in the order it did it: the brief, its words, its steps
 * folded into runs, its report. The body of SubagentStep, and of a running
 * subagent followed from AgentSubagentTracker.
 */
export function SubagentActivity({
  subagent,
  activity,
  subagents,
  onOpenFile,
  brief = true,
  live = false,
  flat = false,
}: {
  subagent: NonNullable<TranscriptItem["subagent"]>;
  activity: TranscriptItem[];
  subagents?: ActivityById;
  onOpenFile?: (path: string, line?: number) => void;
  /** Off where there is no room for a page of instructions above the live steps. */
  brief?: boolean;
  /**
   * Being watched as it runs: every step as its own row. Folded into runs, the
   * step it is on right now sat behind a "Read files, ran commands" line.
   */
  live?: boolean;
  /**
   * No nesting rule and no indent, for a pane of its own (SubagentPanel): the
   * rule belongs to a fold inside the transcript, and in a 420px pane the
   * indent was a quarter of the width.
   */
  flat?: boolean;
}) {
  const segments = React.useMemo(() => workSegments(activity), [activity]);
  return (
    // The rule sits under the row's 14px icon, so the nesting reads as
    // belonging to that line. No w-full: with the margin that would be the
    // column plus 6px, and the report ran past the edge. A flex column's child
    // stretches anyway.
    <div
      data-testid="subagent-activity"
      className={cn("flex min-w-0 flex-col gap-3", !flat && "ml-1.5 border-l border-border-subtle pl-4")}
    >
      {brief && subagent.task ? <SubagentBrief text={subagent.task} /> : null}
      {segments.map((segment) => {
        if (segment.kind === "text") return <AgentProse key={segment.item.id} text={segment.item.text ?? ""} />;
        if (segment.kind === "plan") return <AgentPlan key={segment.item.id} entries={segment.item.plan ?? []} />;
        if (live) {
          return (
            <div key={segment.id} className="flex w-full min-w-0 flex-col gap-2">
              {segment.items.map((item) => (
                <StepRow key={item.id} item={item} subagents={subagents} onOpenFile={onOpenFile} />
              ))}
            </div>
          );
        }
        return (
          <StepGroup
            key={segment.id}
            items={segment.items}
            subagents={subagents}
            onOpenFile={onOpenFile}
            results={NO_RESULTS}
          />
        );
      })}
    </div>
  );
}

/**
 * What the lead agent told the subagent to do. Briefs run to pages — context,
 * tables, the files to read — so the first few lines stand for it until asked.
 */
function SubagentBrief({ text }: { text: string }) {
  const [full, setFull] = React.useState(false);
  // A guess from the text rather than a measurement: a brief of a few short
  // lines fits the well, and a toggle that reveals nothing is a lie.
  const long = text.length > 280 || text.split("\n").length > 4;
  const clamped = long && !full;
  const fade = "linear-gradient(to bottom, black calc(100% - var(--space-page)), transparent)";
  return (
    <div data-testid="subagent-brief" className="flex w-full min-w-0 flex-col gap-1">
      <span className="text-caption-11 text-text-tertiary">Brief</span>
      <div
        className={cn("w-full min-w-0 text-text-secondary", clamped && "max-h-24 overflow-hidden")}
        style={clamped ? { maskImage: fade, WebkitMaskImage: fade } : undefined}
      >
        <AgentProse text={text} />
      </div>
      {long ? (
        <button
          type="button"
          onClick={() => setFull((value) => !value)}
          aria-expanded={full}
          className="self-start cursor-pointer text-caption-11 text-text-tertiary underline decoration-border-subtle hover:text-text-secondary"
        >
          {full ? "Show less" : "Show full brief"}
        </button>
      ) : null}
    </div>
  );
}

/** The second line of an attachment card: what it is, and its pages when it is a PDF. */
function attachmentDetail(a: PromptAttachment): string {
  if (a.kind === "document") return a.pages ? `PDF · ${a.pages} page${a.pages === 1 ? "" : "s"}` : "PDF";
  if (a.kind === "image") return "Image";
  return a.name.includes(".") ? a.name.slice(a.name.lastIndexOf(".") + 1).toUpperCase() : "File";
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
