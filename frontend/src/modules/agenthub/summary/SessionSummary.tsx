import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { Bot, Check, ChevronDown, Database, FileCode, FileText, Globe, Hourglass, PanelRight, Radio, Target, Terminal, type LucideIcon } from "lucide-react";

import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { agentHubListDocuments, type AgentDocument } from "@/bridge/agenthub";
import { openExternal } from "@/bridge/browser";
import { EASE_OUT_CSS, MOTION_BASE } from "@/lib/motion";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { cn } from "@/lib/utils";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { showModuleHome } from "@/workbench/showModule";
import type { CheckpointIndex } from "../checkpoints";
import { isSubagentRunning, sessionRunningSubagents, useSubagentPanel } from "../subagents";
import { subagentName, subagentStateWord } from "../sidebar/model";
import { ChangedFileRow } from "../surfaces/AgentTurnChanges";
import { shortCount } from "../surfaces/AgentContextMeter";
import { loadCheckpointFileDiff } from "../useSessionCheckpoints";
import type { AsyncTask, Connector, PlanEntry, SessionGoal, Subagent, TranscriptItem, Usage } from "../types";
import { asyncTaskSpent } from "../transcript/AgentAsyncTask";
import { useSummarySectionOpen } from "./summaryFold";
import { PullRequestCards } from "./PullRequestCards";
import { hasPullRequests } from "./pullRequestModel";
import type { PullRequestsPanel } from "./usePullRequests";
import {
  backgroundTasks,
  connectorRows,
  isTaskRunning,
  latestPlan,
  limitRows,
  linkHosts,
  sessionChanges,
  tokenRows,
  toolSources,
  type LinkHost,
  type SessionChange,
  type SummaryRow,
  type ToolSource,
} from "./summaryModel";
import { ChangedSource, type ChangedFile } from "../../../../bindings/devkit/internal/agenthub/models";

/** What the summary is derived from. */
export type SessionSummaryInput = {
  sessionId: string;
  items: readonly TranscriptItem[];
  checkpoints: CheckpointIndex;
  live: boolean;
};

export type SessionSummaryData = {
  plan: PlanEntry[];
  changes: SessionChange[];
  tools: ToolSource[];
  links: LinkHost[];
  docs: readonly AgentDocument[];
  /** Running first, then the ones that have finished. */
  subagents: Subagent[];
  /**
   * Background tasks the vendor reported, running first. Empty for a vendor
   * that has no such notion; the section is then not drawn at all.
   */
  background?: AsyncTask[];
};

/** What the card draws: the derived data, plus what it reads as it is. */
export type SessionSummaryProps = {
  sessionId: string;
  checkpoints: CheckpointIndex;
  usage: Usage | null | undefined;
  turnCount: number;
  data: SessionSummaryData;
  /**
   * What the summary shows beyond the transcript, each of it present only when
   * the session has it: where and how it runs, the objective it works towards,
   * the MCP servers a vendor reported. A section a vendor has no data for is
   * not drawn — never drawn empty, or with a placeholder.
   */
  environment?: readonly SummaryRow[];
  goal?: SessionGoal | null;
  connectors?: readonly Connector[];
  /**
   * The pull (or merge) request each repository's branch became, with the
   * reason where one cannot be shown. Absent or empty: no section.
   */
  pullRequests?: PullRequestsPanel;
};

/** PDFs attached to the session's own prompts — each one becomes a document of the session. */
export function documentAttachmentCount(items: readonly TranscriptItem[]): number {
  let count = 0;
  for (const item of items) {
    for (const attachment of item.attachments ?? []) if (attachment.kind === "document") count += 1;
  }
  return count;
}

/** The session's subagents: the running ones, then the tracked ones that have finished. */
export function summarySubagents(
  items: readonly TranscriptItem[],
  tracked: readonly Subagent[] | undefined,
  live: boolean,
): Subagent[] {
  const running = sessionRunningSubagents(items, tracked, live).map((r) => r.subagent);
  const shown = new Set(running.map((a) => a.id));
  const finished = (tracked ?? []).filter((a) => !isSubagentRunning(a.state) && !shown.has(a.id));
  return [...running, ...finished];
}

/**
 * Every derivation the card draws, in one place. Each is memoised on its own
 * input's identity — the transcript publishes at most once a frame, so the
 * plan and the sources are re-read at most that often. Called once per
 * session view (AgentHubContent), whose docked card and corner button share
 * the result, so nothing derives or fetches twice.
 */
export function useSessionSummaryData({ sessionId, items, checkpoints, live }: SessionSummaryInput): SessionSummaryData {
  const plan = React.useMemo(() => latestPlan(items), [items]);
  const changes = React.useMemo(() => sessionChanges(checkpoints), [checkpoints]);
  const tools = React.useMemo(() => toolSources(items), [items]);
  const links = React.useMemo(() => linkHosts(items), [items]);
  const queryKey = React.useMemo(() => ["agenthub", "documents", sessionId], [sessionId]);
  const docs =
    useQuery({
      queryKey,
      queryFn: () => agentHubListDocuments(sessionId),
      enabled: Boolean(sessionId),
      staleTime: 0,
    }).data ?? EMPTY_ARRAY;
  // A PDF attached to a prompt is imported as the turn starts, and no event
  // says so: a new attachment in the transcript is the cue to list again.
  const attached = React.useMemo(() => documentAttachmentCount(items), [items]);
  const queryClient = useQueryClient();
  const seen = React.useRef({ sessionId, attached });
  React.useEffect(() => {
    const previous = seen.current;
    seen.current = { sessionId, attached };
    if (previous.sessionId !== sessionId || previous.attached === attached || !sessionId) return;
    void queryClient.invalidateQueries({ queryKey });
  }, [queryClient, queryKey, sessionId, attached]);
  const tracked = useAgentSessionsStore((s) => s.subagents[sessionId]);
  const subagents = React.useMemo(() => summarySubagents(items, tracked, live), [items, tracked, live]);
  const background = React.useMemo(() => backgroundTasks(items), [items]);
  return React.useMemo(
    () => ({ plan, changes, tools, links, docs, subagents, background }),
    [plan, changes, tools, links, docs, subagents, background],
  );
}

/** What a session's usage has to show: the context window, a token split, plan limits. */
function usageShows(usage: Usage | null | undefined): boolean {
  return Boolean(usage?.size) || Boolean(usage?.tokens && usage.tokens.session.total > 0) || Boolean(usage?.limits?.length);
}

/** Whether any section has something. A card with nothing in it is not drawn. */
export function hasSummary(
  data: SessionSummaryData,
  usage: Usage | null | undefined,
  extras: Pick<SessionSummaryProps, "environment" | "goal" | "connectors" | "pullRequests"> = {},
): boolean {
  return (
    Boolean(extras.environment?.length) ||
    Boolean(extras.goal) ||
    Boolean(extras.connectors?.length) ||
    hasPullRequests(extras.pullRequests) ||
    Boolean(data.background?.length) ||
    data.plan.length > 0 ||
    data.changes.length > 0 ||
    data.tools.length > 0 ||
    data.links.length > 0 ||
    data.docs.length > 0 ||
    data.subagents.length > 0 ||
    usageShows(usage)
  );
}

/** The sections, with their own diff dialog, for a caller that draws neither wrapper. */
export function SessionSummaryBody(props: SessionSummaryProps) {
  const [diffOf, setDiffOf] = React.useState<SessionChange | null>(null);
  return (
    <>
      <SummarySections {...props} onOpenChange={setDiffOf} />
      <ChangeDialog change={diffOf} checkpoints={props.checkpoints} onClose={() => setDiffOf(null)} />
    </>
  );
}

/** Docked at the top-right of a wide document area. */
export function SessionSummaryCard(props: SessionSummaryProps) {
  if (!hasSummary(props.data, props.usage, props)) return null;
  return (
    <section
      aria-label="Session summary"
      data-testid="session-summary-card"
      className="session-summary-enter pointer-events-auto max-h-full w-70 overflow-y-auto overscroll-contain rounded-2xl bg-surface-base py-1.5 shadow-canvas-popover ring-1 ring-border-subtle"
    >
      <SessionSummaryBody {...props} />
    </section>
  );
}

/** The same content behind a round corner button, where the area is too narrow to dock it. */
export function SessionSummaryButton(props: SessionSummaryProps) {
  const [open, setOpen] = React.useState(false);
  const [diffOf, setDiffOf] = React.useState<SessionChange | null>(null);
  const trigger = React.useRef<HTMLButtonElement>(null);
  if (!hasSummary(props.data, props.usage, props)) return null;
  // The diff opens in a dialog outside the popover: the popover closes as the
  // dialog takes focus, and a dialog inside it would close with it. Focus then
  // comes back to the corner button, the one control still on screen.
  const openChange = (change: SessionChange) => {
    setOpen(false);
    setDiffOf(change);
  };
  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            ref={trigger}
            variant="ghost"
            size="icon-sm"
            aria-label="Session summary"
            data-testid="session-summary-button"
            className="rounded-full bg-surface-base shadow-canvas-chip ring-1 ring-border-subtle"
          >
            <PanelRight aria-hidden="true" />
          </Button>
        </PopoverTrigger>
        <PopoverContent
          side="bottom"
          align="end"
          aria-label="Session summary"
          className="max-h-screen-85 w-70 gap-0 overflow-y-auto p-0 py-1.5"
        >
          <SummarySections {...props} onOpenChange={openChange} />
        </PopoverContent>
      </Popover>
      <ChangeDialog
        change={diffOf}
        checkpoints={props.checkpoints}
        onClose={() => setDiffOf(null)}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          trigger.current?.focus();
        }}
      />
    </>
  );
}

const VISIBLE_ROWS = 5;
const LIST_MOTION = { duration: MOTION_BASE * 1000, easing: EASE_OUT_CSS };
const ROW = "flex h-7.5 w-full min-w-0 shrink-0 items-center gap-2.5 px-3.5 text-left text-body-13 text-text-primary";
const BUTTON_ROW = cn(
  ROW,
  "cursor-pointer transition-control hover:bg-state-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-border-focus/50",
);
const ICON = "size-(--icon-14) shrink-0 text-text-tertiary";
const TRAILING = "ml-auto shrink-0 text-caption-11 tabular-nums text-text-tertiary";

function SummarySections({
  sessionId,
  usage,
  turnCount,
  data,
  environment = [],
  goal = null,
  connectors = [],
  pullRequests,
  onOpenChange,
}: SessionSummaryProps & { onOpenChange: (change: SessionChange) => void }) {
  if (!hasSummary(data, usage, { environment, goal, connectors, pullRequests })) return null;
  const { plan, changes, tools, links, docs, subagents } = data;
  const background = data.background ?? [];
  const done = plan.filter((entry) => entry.status === "completed").length;
  const added = changes.reduce((sum, change) => sum + change.added, 0);
  const removed = changes.reduce((sum, change) => sum + change.removed, 0);
  const connectorList = connectorRows(connectors);

  return (
    <>
      {environment.length ? (
        <SummarySection
          name="environment"
          title="Environment"
          rows={environment.map((row) => (
            <ValueRow key={row.key} row={row} testId="summary-environment-row" />
          ))}
          visibleRows={environment.length}
        />
      ) : null}

      {goal ? (
        <SectionShell name="goal" title="Goal">
          <div className="flex items-start gap-2.5 px-3.5 pt-0.5 pb-2 text-body-13 text-text-primary">
            <Target aria-hidden="true" className={cn(ICON, "mt-0.5")} />
            <span className="min-w-0 wrap-break-word" title={goal.objective}>
              {goal.objective}
            </span>
          </div>
        </SectionShell>
      ) : null}

      {plan.length ? (
        <SummarySection
          name="plan"
          title="Plan"
          count={`${done}/${plan.length}`}
          rows={plan.map((entry, i) => (
            // By position: a step's text may be rewritten by the next plan.
            <PlanRow key={i} entry={entry} />
          ))}
          windowStart={planWindowStart(plan)}
        />
      ) : null}

      {changes.length ? (
        <SummarySection
          name="changes"
          title="Changes"
          count={changes.length}
          aside={
            <span className="ml-auto flex gap-1 text-caption-11 tabular-nums" data-testid="summary-changes-total">
              <span className="text-status-success">{`+${added.toLocaleString("en-US")}`}</span>
              <span className="text-status-danger">{`−${removed.toLocaleString("en-US")}`}</span>
            </span>
          }
          rows={changes.map((change) => (
            <ChangeRow key={change.path} change={change} onOpen={() => onOpenChange(change)} />
          ))}
        />
      ) : null}

      {pullRequests && hasPullRequests(pullRequests) ? (
        // Beside Changes, not inside it: Changes lists files and is empty until
        // a turn has been checkpointed, while a request exists regardless; and
        // its +/− total is files', which a request's size must not be added to.
        <SectionShell
          name="pullRequests"
          title={pullRequestsTitle(pullRequests)}
          count={pullRequests.cards.length || undefined}
        >
          <PullRequestCards panel={pullRequests} several={new Set(pullRequests.cards.map((c) => c.repo)).size > 1} />
        </SectionShell>
      ) : null}

      {background.length ? (
        <SummarySection
          name="background"
          title="Background tasks"
          count={background.length}
          rows={background.map((task) => (
            <BackgroundTaskRow key={task.id} task={task} />
          ))}
        />
      ) : null}

      {docs.length || tools.length || links.length ? (
        <SummarySection
          name="sources"
          title="Sources"
          count={docs.length + tools.length + links.length}
          rows={[
            ...docs.map((doc) => (
              <div key={`doc:${doc.id}`} data-testid="summary-source-row" className={ROW} title={doc.name}>
                <FileText aria-hidden="true" className="size-(--icon-14) shrink-0 text-status-danger" />
                <span className="min-w-0 truncate">{doc.name}</span>
                <span className={TRAILING}>{doc.pages === 1 ? "1 page" : `${doc.pages} pages`}</span>
              </div>
            )),
            ...tools.map((tool) => {
              const Icon = TOOL_ICON[tool.module] ?? Database;
              return (
                <button
                  key={`tool:${tool.module}`}
                  type="button"
                  data-testid="summary-source-row"
                  className={BUTTON_ROW}
                  onClick={() => showModuleHome(tool.module)}
                >
                  <Icon aria-hidden="true" className={ICON} />
                  <span className="min-w-0 truncate">{tool.label}</span>
                  <span className={TRAILING}>{tool.detail}</span>
                </button>
              );
            }),
            ...links.map((link) => (
              <button
                key={`link:${link.host}`}
                type="button"
                data-testid="summary-source-row"
                title={link.url}
                className={BUTTON_ROW}
                onClick={() => void openExternal(link.url)}
              >
                <Globe aria-hidden="true" className={ICON} />
                <span className="min-w-0 truncate">{link.host}</span>
              </button>
            )),
          ]}
        />
      ) : null}

      {subagents.length ? (
        <SummarySection
          name="subagents"
          title="Subagents"
          count={subagents.length}
          rows={subagents.map((subagent) => (
            <button
              key={subagent.id}
              type="button"
              data-testid="summary-subagent-row"
              title={subagent.task}
              className={BUTTON_ROW}
              onClick={() => useSubagentPanel.getState().show(sessionId, subagent.id)}
            >
              <Bot aria-hidden="true" className={ICON} />
              <span className="min-w-0 truncate">{subagentName(subagent)}</span>
              <span className={TRAILING}>{subagentStateWord(subagent.state)}</span>
            </button>
          ))}
        />
      ) : null}

      {connectorList.length ? (
        <SummarySection
          name="connectors"
          title="Connectors"
          count={connectorList.length}
          rows={connectorList.map((row) => (
            <ValueRow key={row.key} row={row} testId="summary-connector-row" />
          ))}
        />
      ) : null}

      {usageShows(usage) && usage ? <ContextSection usage={usage} turnCount={turnCount} /> : null}
    </>
  );
}

/** "Pull requests", or "Merge requests" when that is the only word the platforms in use have. */
function pullRequestsTitle(panel: PullRequestsPanel): string {
  const shorts = new Set(panel.cards.map((card) => card.short));
  return shorts.size === 1 && shorts.has("MR") ? "Merge requests" : "Pull requests";
}

const TOOL_ICON: Record<string, LucideIcon> = { database: Database, kafka: Radio, ssh: Terminal };

/**
 * A titled list that shows five rows and folds the rest behind "See all".
 * Rows arriving while the session runs slide in (auto-animate, --motion-base).
 */
function SummarySection({
  name,
  title,
  count,
  aside,
  rows,
  windowStart = 0,
  visibleRows = VISIBLE_ROWS,
}: {
  name: string;
  title: string;
  count?: React.ReactNode;
  /** Right-aligned in the heading — a total that belongs to the whole list. */
  aside?: React.ReactNode;
  rows: React.ReactNode[];
  /** The first row shown while folded; the rows above it unfold above. */
  windowStart?: number;
  /** How many rows show before the rest fold behind "See all". */
  visibleRows?: number;
}) {
  const [expanded, setExpanded] = React.useState(false);
  const [listRef] = useAutoAnimate<HTMLDivElement>(LIST_MOTION);
  const [restRef] = useAutoAnimate<HTMLDivElement>(LIST_MOTION);
  const start = Math.max(0, Math.min(windowStart, rows.length - visibleRows));
  const before = rows.slice(0, start);
  const head = rows.slice(start, start + visibleRows);
  const rest = rows.slice(start + visibleRows);
  return (
    <SectionShell name={name} title={title} count={count} aside={aside}>
      {before.length ? (
        <AnimatedCollapse open={expanded}>
          <div className="flex flex-col">{before}</div>
        </AnimatedCollapse>
      ) : null}
      <div ref={listRef} className="flex flex-col">
        {head}
      </div>
      {before.length || rest.length ? (
        <>
          <AnimatedCollapse open={expanded}>
            <div ref={restRef} className="flex flex-col">
              {rest}
            </div>
          </AnimatedCollapse>
          <button
            type="button"
            data-testid="summary-see-all"
            aria-expanded={expanded}
            onClick={() => setExpanded((value) => !value)}
            className={cn(BUTTON_ROW, "text-text-tertiary hover:text-text-secondary")}
          >
            {expanded ? "Show less" : `See all (${rows.length})`}
          </button>
        </>
      ) : null}
    </SectionShell>
  );
}

/**
 * A titled section the person can fold (shadcn Collapsible). The title row is
 * the trigger; a total that belongs to the whole list sits beside the chevron.
 * Folded sections are remembered (summaryFold), the same for the docked card
 * and the corner popover.
 */
function SectionShell({
  name,
  title,
  count,
  aside,
  children,
}: {
  name: string;
  title: string;
  count?: React.ReactNode;
  aside?: React.ReactNode;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useSummarySectionOpen(name);
  return (
    <Collapsible
      asChild
      open={open}
      onOpenChange={setOpen}
    >
      <section data-testid={`summary-${name}`} data-open={open} className="flex flex-col border-t border-border-subtle first:border-t-0">
        <h4 className="flex">
          <CollapsibleTrigger
            data-testid={`summary-${name}-toggle`}
            className="group flex h-8 w-full min-w-0 cursor-pointer items-center gap-1 px-3.5 text-left text-body-13 text-text-secondary transition-control hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-border-focus/50"
          >
            <span className="truncate">{title}</span>
            {count !== undefined ? <sup className="text-caption-11 text-text-tertiary/70">{count}</sup> : null}
            {aside}
            <ChevronDown
              aria-hidden="true"
              className={cn(
                "size-(--icon-14) shrink-0 text-text-tertiary transition-transform duration-(--motion-base) ease-(--ease-out-strong) motion-reduce:transition-none",
                aside ? "ml-2" : "ml-auto",
                !open && "-rotate-90",
              )}
            />
          </CollapsibleTrigger>
        </h4>
        <CollapsibleContent>
          <div className="flex flex-col">{children}</div>
        </CollapsibleContent>
      </section>
    </Collapsible>
  );
}

/**
 * Where a long plan's five visible steps start: at the top until the step in
 * progress would fall below them, then one done step above it for context.
 */
function planWindowStart(plan: readonly PlanEntry[]): number {
  const now = plan.findIndex((entry) => entry.status === "in_progress");
  return now < VISIBLE_ROWS ? 0 : Math.min(now - 1, plan.length - VISIBLE_ROWS);
}

const TONE_TEXT = { ok: "text-status-success", warning: "text-status-warning", danger: "text-status-danger" } as const;

/** A name on the left and what it is on the right; the tone is how safe it is. */
function ValueRow({ row, testId }: { row: SummaryRow; testId: string }) {
  return (
    <div data-testid={testId} data-tone={row.tone} title={row.title} className={ROW}>
      <span className="shrink-0 text-text-secondary">{row.label}</span>
      <span className={cn("ml-auto min-w-0 truncate text-right", row.tone ? TONE_TEXT[row.tone] : "text-text-primary")}>{row.value}</span>
    </div>
  );
}

/** One background task: its name, what it is doing or found, and what it has spent. */
function BackgroundTaskRow({ task }: { task: AsyncTask }) {
  const running = isTaskRunning(task);
  const name = task.name || task.description || "Background task";
  const detail = running ? task.lastToolName : task.summary;
  const spent = asyncTaskSpent(task);
  return (
    <div
      data-testid="summary-background-row"
      data-state={task.state || "running"}
      title={[name, detail, spent].filter(Boolean).join(" · ")}
      className={ROW}
    >
      <Hourglass aria-hidden="true" className={ICON} />
      <span className="min-w-0 truncate">{name}</span>
      <span className={TRAILING}>{running ? spent || "running" : task.state}</span>
    </div>
  );
}

function PlanRow({ entry }: { entry: PlanEntry }) {
  const state = entry.status === "completed" || entry.status === "in_progress" ? entry.status : "pending";
  return (
    <div data-testid="summary-plan-row" data-state={state} className={ROW}>
      <span
        aria-hidden="true"
        className={cn(
          "flex size-3.5 shrink-0 items-center justify-center rounded-full transition-colors duration-(--motion-fast) motion-reduce:transition-none",
          state === "completed" && "bg-text-primary text-surface-base",
          state === "in_progress" && "summary-step-ring text-text-primary",
          state === "pending" && "summary-step-ring text-text-tertiary",
        )}
      >
        {state === "completed" ? <Check className="size-2.5" strokeWidth={3} /> : null}
      </span>
      <span
        className={cn(
          "min-w-0 truncate transition-colors duration-(--motion-fast) motion-reduce:transition-none",
          state === "completed" && "text-text-tertiary",
        )}
        title={entry.content}
      >
        {entry.content}
      </span>
      <span className="sr-only">{state === "completed" ? "done" : state === "in_progress" ? "in progress" : "pending"}</span>
    </div>
  );
}

/** The file name first, its folder after it dimmed: truncating the full path from the right would cut the name. */
function ChangeRow({ change, onOpen }: { change: SessionChange; onOpen: () => void }) {
  const cut = change.path.lastIndexOf("/");
  const name = cut >= 0 ? change.path.slice(cut + 1) : change.path;
  const dir = cut >= 0 ? change.path.slice(0, cut) : "";
  const content = (
    <>
      <FileCode aria-hidden="true" className={ICON} />
      <span className="shrink-0 truncate">{name}</span>
      {dir ? <span className="min-w-0 flex-1 truncate text-caption-11 text-text-tertiary">{dir}</span> : <span className="flex-1" />}
      {change.binary ? (
        <span className="shrink-0 text-caption-11 text-text-tertiary">binary</span>
      ) : (
        <span className="flex shrink-0 gap-1 text-caption-11 tabular-nums">
          <span className="text-status-success">{`+${change.added}`}</span>
          <span className="text-status-danger">{`−${change.removed}`}</span>
        </span>
      )}
    </>
  );
  // A binary file has no diff to show, so it is not offered as a control.
  if (change.binary) {
    return (
      <div data-testid="summary-change-row" title={change.path} className={ROW}>
        {content}
      </div>
    );
  }
  return (
    <button type="button" data-testid="summary-change-row" title={change.path} className={BUTTON_ROW} onClick={onOpen}>
      {content}
    </button>
  );
}

function formatCost(amount: number, currency?: string): string {
  try {
    return new Intl.NumberFormat("en-US", { style: "currency", currency: currency || "USD" }).format(amount);
  } catch {
    return `$${amount.toFixed(2)}`;
  }
}

function ContextSection({ usage, turnCount }: { usage: Usage; turnCount: number }) {
  const percent = usage.size > 0 ? Math.min(100, Math.round((usage.used / usage.size) * 100)) : null;
  const parts = [`${turnCount} ${turnCount === 1 ? "turn" : "turns"}`];
  if (percent !== null) parts.unshift(`${percent}%`);
  if (usage.amount > 0) parts.push(formatCost(usage.amount, usage.currency));
  const tokens = tokenRows(usage.tokens);
  const limits = limitRows(usage.limits);
  return (
    <SectionShell name="context" title="Context">
      {percent !== null ? (
        <div
          className="flex flex-col gap-1.5 px-3.5 pt-1 pb-2"
          title={`${shortCount(usage.used)} of ${shortCount(usage.size)} tokens`}
        >
          <div
            role="meter"
            aria-label="Context window used"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={percent}
            className="h-1 w-full overflow-hidden rounded-full bg-state-soft"
          >
            <div
              className="h-full w-full origin-left rounded-full bg-text-primary transition-transform duration-(--motion-base) ease-(--ease-out-strong) motion-reduce:transition-none"
              style={{ transform: `scaleX(${percent / 100})` }}
            />
          </div>
          <span className="text-caption-11 tabular-nums text-text-tertiary">{parts.join(" · ")}</span>
        </div>
      ) : null}
      {tokens.map((row) => (
        <ValueRow key={row.key} row={row} testId="summary-token-row" />
      ))}
      {limits.length ? (
        <>
          <h5 className="px-3.5 pt-1.5 pb-0.5 text-caption-11 text-text-tertiary">Plan limits</h5>
          {limits.map((row) => (
            <ValueRow key={row.key} row={row} testId="summary-limit-row" />
          ))}
        </>
      ) : null}
    </SectionShell>
  );
}

/**
 * The checkpoint's own entry for the path: it knows the file's kind and who
 * wrote it, which the summed row does not keep. A row the index no longer has
 * (swept since) still opens, unattributed.
 */
function changedFileOf(checkpoints: CheckpointIndex, change: SessionChange): ChangedFile {
  for (const record of checkpoints.values()) {
    if (record.id !== change.checkpointId) continue;
    const file = record.changed?.find((f) => f.path === change.path);
    if (file) return file;
  }
  return {
    path: change.path,
    kind: "modified",
    added: change.added,
    removed: change.removed,
    binary: change.binary,
    source: ChangedSource.SourceUnattributed,
  };
}

/**
 * One file's diff from the newest checkpoint that touched it, in the same row
 * a turn's changes use. The row's counts are summed over the session, the
 * diff is that last turn's alone, and the title says so.
 */
function ChangeDialog({
  change,
  checkpoints,
  onClose,
  onCloseAutoFocus,
}: {
  change: SessionChange | null;
  checkpoints: CheckpointIndex;
  onClose: () => void;
  onCloseAutoFocus?: (event: Event) => void;
}) {
  // Kept while the dialog closes, so its exit can play with the content still in it.
  // (Adjusted during render, React's pattern for state that follows a prop.)
  const [shown, setShown] = React.useState(change);
  if (change && change !== shown) setShown(change);
  const name = shown ? shown.path.slice(shown.path.lastIndexOf("/") + 1) : "";
  return (
    <Dialog open={change !== null} onOpenChange={(open) => !open && onClose()}>
      {shown ? (
        <DialogContent
          data-testid="summary-change-dialog"
          onCloseAutoFocus={onCloseAutoFocus}
          className="max-h-screen-85 overflow-y-auto sm:max-w-3xl"
        >
          <DialogTitle className="text-body-13 text-text-primary">{`Last change to ${name}`}</DialogTitle>
          <DialogDescription className="text-caption-11 text-text-tertiary">
            From the latest turn that touched it.
          </DialogDescription>
          <ChangedFileRow
            checkpointId={shown.checkpointId}
            file={changedFileOf(checkpoints, shown)}
            overlap={null}
            loadDiff={loadCheckpointFileDiff}
            defaultOpen
          />
        </DialogContent>
      ) : null}
    </Dialog>
  );
}
