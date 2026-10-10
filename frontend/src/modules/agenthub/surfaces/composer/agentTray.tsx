import * as React from "react";
import {
  ArrowUpRight,
  Bot,
  Boxes,
  CheckCircle2,
  ChevronDown,
  Circle,
  CircleDot,
  Eye,
  Folder,
  GitBranch,
  ListTodo,
  MessageCircleQuestion,
  ShieldAlert,
  Terminal,
} from "lucide-react";

import type {
  AgentSession,
  ElicitationResponse,
  PendingElicitation,
  PendingPermission,
} from "@/bridge/agenthub";
import { dispatchIntent } from "@/workbench/intents";
import { AnimatedNumber } from "@/components/ui/animated-number";
import { EmptyState } from "@/components/ui/empty-state";
import { TrayPanelHeading } from "./TrayPanelHeading";
import { cn } from "@/lib/utils";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { sessionRunningSubagents } from "../../subagents";
import type { TranscriptItem } from "../../types";
import { AgentPermissionRequest } from "../AgentPermissionRequest";
import { AgentQuestionCard } from "../AgentQuestionCard";
import { AgentSubagentTracker } from "../AgentSubagentTracker";
import { TrayChip, TrayChips, type TrayPanel } from "./ComposerTray";
import {
  ComposerContextBar,
  type ContextBarMenuEntry,
} from "./ComposerContextBar";
import { chooseTrayPanel, repoSummary, type TrayOpened } from "./trayModel";
import { useRisingEdge } from "../../sidebar/hooks";
import {
  SLOW_COMMAND_MS,
  commandText,
  formatElapsed,
  isRunningCommand,
  useElapsed,
} from "../../transcript/AgentRunningCommand";
import {
  repoBranch,
  useTrayStatus,
  type TrayStatus,
  type TrayTask,
} from "./useTrayStatus";
import { useFollowing } from "../../follow";

export type AgentTrayInput = {
  size: "wide" | "rail";
  session?: AgentSession | null;
  /** The folder this work is in — the session's, or the draft's pick. */
  projectPath: string;
  /** A turn is running: only then is a subagent without an end still running. */
  live: boolean;
  items: TranscriptItem[];
  onOpenFile?: (path: string, line?: number) => void;
  /** Stands in for the workspace chip — the draft's folder picker, while there is still a folder to pick. */
  leading?: React.ReactNode;
  permission?: {
    pending: PendingPermission;
    onResolve: (optionId: string | null) => void | Promise<void>;
    busy?: boolean;
  } | null;
  question?: {
    pending: PendingElicitation;
    onRespond: (response: ElicitationResponse) => void | Promise<void>;
    busy?: boolean;
    error?: string;
  } | null;
};

function folderName(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  return (
    trimmed.slice(
      Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\")) + 1,
    ) || trimmed
  );
}

/**
 * Everything the composer cluster shows for one conversation (see
 * ComposerPanelArea and ComposerContextBar): the bar's content and the panel
 * above it, with the rule for which panel wins (chooseTrayPanel).
 *
 * One panel at a time, first of:
 * 1. a permission the agent is waiting on,
 * 2. a question it is waiting on,
 * 3. whatever the person opened from the bar (subagents, tasks, repositories).
 * (An "@" or "/" list being typed goes above all of these — the composer
 * decides that, since it is the one that knows.)
 *
 * A permission or question can be folded into the bar to read the transcript
 * that explains it: folding is remembered per request, so a new request opens
 * on its own, and the folded one stays in the bar as a chip — named, counted,
 * with a dot that beats a few times on arrival and then rests — that opens it
 * again: the agent is still waiting. More than one waiting says how many, and
 * answering one brings the next.
 *
 * Direct on the bar: where this is, what blocks the agent, repositories, work
 * running right now. Everything else — Tasks, Subagents, Commands, Follow — is
 * in the bar's "…" menu, so a quiet session shows a quiet bar.
 */
export function useAgentTray(input: AgentTrayInput): {
  strip: React.ReactNode;
  panel: TrayPanel | null;
} {
  const {
    size,
    session,
    projectPath,
    live,
    items,
    onOpenFile,
    leading,
    permission,
    question,
  } = input;
  const status = useTrayStatus(
    projectPath,
    session?.id ?? "",
    `${session?.id}:${live}`,
  );
  // Running by the tracked state, not only while the turn runs: a lead agent
  // that ends its turn to wait on background subagents used to make the chip
  // vanish while they were still working (and the sidebar said so).
  const tracked = useAgentSessionsStore((s) =>
    session ? s.subagents[session.id] : undefined,
  );
  const running = React.useMemo(
    () => sessionRunningSubagents(items, tracked, live).length,
    [items, tracked, live],
  );
  // How many of each this session has open, off the queues: the view hands over
  // the first, and "1 of 3" is what tells the person there is more behind it.
  // Numbers, so a queue changing under another session re-renders nothing here.
  const sessionId = session?.id ?? "";
  const permissionsWaiting = useAgentSessionsStore((s) =>
    sessionId
      ? s.pendingQueue.filter((p) => p.sessionId === sessionId).length
      : 0,
  );
  const questionsWaiting = useAgentSessionsStore((s) =>
    sessionId
      ? s.elicitationQueue.filter((q) => q.sessionId === sessionId).length
      : 0,
  );
  const permissionCount = permission ? Math.max(1, permissionsWaiting) : 0;
  const questionCount = question ? Math.max(1, questionsWaiting) : 0;
  // A request ARRIVING beats the chip's dot a few times; a rerender, a switch
  // of selection or folding one away does not (useRisingEdge only counts a rise
  // seen while mounted).
  const arrived = useRisingEdge(permissionCount + questionCount);
  // Shell commands still running, oldest first — the one the clock reads.
  const commands = React.useMemo(
    () => (live ? items.filter(isRunningCommand) : []),
    [items, live],
  );
  const commandsFor = useElapsed(commands[0]?.createdAt, commands.length > 0);
  const slowCommands = commands.length > 0 && commandsFor >= SLOW_COMMAND_MS;
  const [opened, setOpened] = React.useState<TrayOpened>(null);
  const [following, toggleFollowing] = useFollowing(session?.id);
  const [folded, setFolded] = React.useState<ReadonlySet<string>>(
    () => new Set(),
  );

  const fold = (id: string) => setFolded((prev) => new Set(prev).add(id));
  const unfold = (id: string) =>
    setFolded((prev) => {
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  const toggle = (panel: Exclude<TrayOpened, null>) =>
    setOpened((current) => (current === panel ? null : panel));

  const showing = chooseTrayPanel({
    permission: Boolean(permission),
    permissionFolded: Boolean(permission && folded.has(permission.pending.id)),
    question: Boolean(question),
    questionFolded: Boolean(question && folded.has(question.pending.id)),
    opened,
    runningSubagents: running,
    hasBoards: status.hasBoards,
    repoCount: status.repos.length,
    slowCommands,
  });
  const closeOpened = () => setOpened(null);

  /** "2 more waiting after this one" under a request, when there are. */
  const more = (n: number) =>
    n > 1 ? (
      <p
        data-testid="tray-more-waiting"
        className="border-t border-border-subtle px-3 py-1.5 text-caption-11 text-text-tertiary"
      >
        {`${n - 1} more waiting after this one`}
      </p>
    ) : null;

  let panel: TrayPanel | null = null;
  if (showing === "permission" && permission) {
    panel = {
      key: `permission:${permission.pending.id}`,
      onDismiss: () => fold(permission.pending.id),
      content: (
        <div className="composer-panel-cap overflow-y-auto overscroll-contain">
          <AgentPermissionRequest
            pending={permission.pending}
            session={session}
            onResolve={permission.onResolve}
            busy={permission.busy}
            size={size}
            onMinimize={() => fold(permission.pending.id)}
          />
          {more(permissionCount)}
        </div>
      ),
    };
  } else if (showing === "question" && question) {
    panel = {
      key: `question:${question.pending.id}`,
      onDismiss: () => fold(question.pending.id),
      content: (
        <div className="composer-panel-cap overflow-y-auto overscroll-contain">
          <AgentQuestionCard
            pending={question.pending}
            session={session}
            onRespond={question.onRespond}
            busy={question.busy}
            error={question.error}
            size={size}
            onMinimize={() => fold(question.pending.id)}
          />
          {more(questionCount)}
        </div>
      ),
    };
  } else if (showing === "subagents") {
    panel = {
      key: "subagents",
      onDismiss: closeOpened,
      content: (
        <>
          <TrayPanelHeading
            title="Subagents"
            count={running}
            detail="Work running alongside this conversation"
            onClose={closeOpened}
          />
          <AgentSubagentTracker
            items={items}
            live={live}
            tracked={tracked}
            onOpenFile={onOpenFile}
            variant="tray"
          />
        </>
      ),
    };
  } else if (showing === "commands") {
    panel = {
      key: "commands",
      onDismiss: closeOpened,
      content: <TrayCommandList commands={commands} onClose={closeOpened} />,
    };
  } else if (showing === "repos") {
    panel = {
      key: "repos",
      onDismiss: closeOpened,
      content: <TrayRepoList repos={status.repos} onClose={closeOpened} />,
    };
  } else if (showing === "tasks") {
    panel = {
      key: "tasks",
      onDismiss: closeOpened,
      content: (
        <TrayTaskList
          tasks={status.tasks}
          others={status.others}
          session={Boolean(session)}
          onClose={closeOpened}
        />
      ),
    };
  }

  const repos = repoSummary(status.repos, status.branch);
  const workspaceName = status.workspace?.name;
  const where = leading ? (
    <span key="where" className="flex min-w-0 shrink">
      {leading}
    </span>
  ) : (
    <TrayChip
      key="where"
      testId="tray-workspace"
      icon={status.workspace ? <Boxes /> : <Folder />}
      label={
        workspaceName ?? (projectPath ? folderName(projectPath) : "No project")
      }
      keepLabel
      // The workspace's folders are in the tooltip, not the label: "name (3)"
      // read like a count of something else.
      title={
        status.workspace
          ? `${status.workspace.name} · ${status.workspace.folders} folder${status.workspace.folders === 1 ? "" : "s"}${projectPath ? `\n${projectPath}` : ""}`
          : projectPath || undefined
      }
    />
  );

  // What is waiting and not on screen — folded away, or behind something else.
  const waitingChips = (
    <>
      {permission && showing !== "permission" ? (
        <TrayChip
          key="permission"
          testId="tray-permission"
          icon={<ShieldAlert />}
          label="Permission"
          count={permissionCount > 1 ? permissionCount : undefined}
          title={`The agent is waiting for ${permissionCount > 1 ? `${permissionCount} permissions` : "a permission"} — open ${permissionCount > 1 ? "them" : "it"}`}
          tone="attention"
          pulse={arrived}
          priority
          onClick={() => {
            unfold(permission.pending.id);
            setOpened(null);
          }}
        />
      ) : null}
      {question && showing !== "question" ? (
        <TrayChip
          key="question"
          testId="tray-question"
          icon={<MessageCircleQuestion />}
          label="Question"
          count={questionCount > 1 ? questionCount : undefined}
          title={`The agent is waiting on ${questionCount > 1 ? `${questionCount} questions` : "a question"} — open ${questionCount > 1 ? "them" : "it"}`}
          tone="attention"
          pulse={arrived}
          priority
          onClick={() => {
            unfold(question.pending.id);
            setOpened(null);
            // Behind a permission, the question only comes forward once that
            // is folded: the permission is the one blocking the agent.
            if (permission) fold(permission.pending.id);
          }}
        />
      ) : null}
    </>
  );

  const chips = (
    <TrayChips>
      {waitingChips}
      {status.repos.length > 0 ? (
        <TrayChip
          key="branch"
          testId="tray-branch"
          icon={<GitBranch />}
          // One repository: its branch, as it always was. Several (a workspace
          // of services): how many — and, as a badge of its own, how many of
          // them need a look. The two numbers used to share one pill.
          label={repos.label}
          badge={repos.badge ?? undefined}
          title={`Show repositories · ${repos.title}`}
          active={panel?.key === "repos"}
          onClick={() => toggle("repos")}
        />
      ) : null}
      {slowCommands ? (
        <TrayChip
          key="commands"
          testId="tray-commands"
          icon={<Terminal />}
          // The clock of the longest-running one: what a long build is
          // waiting on, in view next to where the next message is typed.
          label={formatElapsed(commandsFor)}
          count={commands.length > 1 ? commands.length : undefined}
          title={`${commands.length} command${commands.length === 1 ? "" : "s"} running — ${commandText(commands[0].toolCall!)}`}
          active={panel?.key === "commands"}
          onClick={() => toggle("commands")}
        />
      ) : null}
      {running > 0 ? (
        <TrayChip
          key="subagents"
          testId="tray-subagents"
          icon={<Bot />}
          label={running === 1 ? "subagent" : "subagents"}
          count={running}
          title={`${running} subagent${running === 1 ? "" : "s"} running`}
          active={panel?.key === "subagents"}
          onClick={() => toggle("subagents")}
        />
      ) : null}
    </TrayChips>
  );

  // The rest, behind "…". Tasks stays even at 0: no task yet is not no tasks
  // feature, and the empty list says how to get one. Subagents has nothing to
  // open while none runs, so it is there and quiet.
  const menu: ContextBarMenuEntry[] = [];
  if (status.hasBoards) {
    const count = session ? status.tasks.length : status.others.length;
    menu.push({
      kind: "item",
      key: "tasks",
      testId: "tray-menu-tasks",
      icon: <ListTodo />,
      label: "Tasks",
      // This session's tasks, not the workspace's: every session of a
      // workspace used to show the same number.
      detail: String(count),
      active: panel?.key === "tasks",
      onSelect: () => toggle("tasks"),
    });
  }
  if (session) {
    menu.push({
      kind: "item",
      key: "subagents",
      testId: "tray-menu-subagents",
      icon: <Bot />,
      label: "Subagents",
      detail: running > 0 ? String(running) : "none running",
      disabled: running === 0,
      active: panel?.key === "subagents",
      onSelect: () => toggle("subagents"),
    });
    if (commands.length > 0) {
      menu.push({
        kind: "item",
        key: "commands",
        testId: "tray-menu-commands",
        icon: <Terminal />,
        label: "Running commands",
        detail: String(commands.length),
        disabled: !slowCommands,
        active: panel?.key === "commands",
        onSelect: () => toggle("commands"),
      });
    }
    // Follow: what this agent changes in DevKit shows as it happens — the
    // activity panel in Agent Hub, a toast anywhere else (follow.tsx). A session
    // that does not exist yet (a draft) has nothing to follow.
    menu.push({
      kind: "toggle",
      key: "follow",
      testId: "tray-follow",
      icon: <Eye />,
      label: "Follow changes",
      checked: following,
      onToggle: toggleFollowing,
    });
  }

  const strip = (
    <ComposerContextBar
      compact={size === "rail"}
      where={where}
      chips={chips}
      menu={menu}
      following={following}
    />
  );

  return { strip, panel };
}

/**
 * This session's tasks, then — apart, under their own label — the rest of the
 * workspace's open ones. A row opens the task beside the conversation
 * (tasks.reveal), the way a file mentioned in it does.
 */
export function TrayTaskList({
  tasks,
  others,
  session,
  onClose,
}: {
  tasks: TrayTask[];
  others: TrayTask[];
  session: boolean;
  onClose?: () => void;
}) {
  const completed = tasks.filter((row) => row.done).length;
  const count = session ? tasks.length : others.length;
  return (
    <section data-testid="tray-task-list" className="flex min-w-0 flex-col">
      <TrayPanelHeading
        title={session ? "Session tasks" : "Workspace tasks"}
        count={count}
        detail={
          session
            ? "Work created or picked up by this session"
            : "Open a task beside this conversation"
        }
        onClose={onClose}
      />
      {session && tasks.length > 0 ? (
        <div className="border-b border-border-subtle px-3 py-2.5">
          <div className="mb-2 flex justify-between text-caption-11 text-text-tertiary">
            <span>This session</span>
            <span>
              <AnimatedNumber value={completed} /> of {tasks.length} completed
            </span>
          </div>
          <div
            role="progressbar"
            aria-label="Completed session tasks"
            aria-valuenow={completed}
            aria-valuemin={0}
            aria-valuemax={tasks.length}
            className="h-1 overflow-hidden rounded-full bg-state-soft"
          >
            <div
              className="h-full origin-left rounded-full bg-status-success transition-transform duration-(--motion-base) ease-(--ease-out-strong) motion-reduce:transition-none"
              style={{ transform: `scaleX(${completed / tasks.length})` }}
            />
          </div>
        </div>
      ) : null}
      <div className="max-h-64 overflow-y-auto overscroll-contain p-2 scrollbar-gutter-stable">
        {tasks.length === 0 && others.length === 0 ? (
          <EmptyState
            size="rail"
            icon={ListTodo}
            title="No tasks yet"
            body="No open tasks on this workspace’s boards."
          />
        ) : (
          <>
            {session ? (
              tasks.length > 0 ? (
                <TrayTaskRows tasks={tasks} testId="tray-task" />
              ) : (
                <p className="px-2 py-3 text-body-13 text-text-tertiary">
                  No tasks from this session yet
                </p>
              )
            ) : null}
            {others.length > 0 ? (
              session ? (
                <details className="group/workspace mt-2 border-t border-border-subtle pt-2">
                  <summary className="flex cursor-pointer list-none items-center gap-2 rounded-md px-2 py-2 text-caption-11-medium text-text-secondary transition-control hover:bg-state-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-status-info/30 [&::-webkit-details-marker]:hidden">
                    <ChevronDown className="size-3.5 -rotate-90 transition-transform duration-(--motion-base) group-open/workspace:rotate-0 motion-reduce:transition-none" />
                    Open in the workspace
                    <AnimatedNumber
                      value={others.length}
                      className="ml-auto tabular-nums text-text-tertiary"
                    />
                  </summary>
                  <TrayTaskRows tasks={others} testId="tray-task-other" />
                </details>
              ) : (
                <TrayTaskRows tasks={others} testId="tray-task-other" />
              )
            ) : null}
          </>
        )}
      </div>
    </section>
  );
}

function TrayTaskRows({
  tasks,
  testId,
}: {
  tasks: TrayTask[];
  testId: string;
}) {
  return (
    <ul className="space-y-1">
      {tasks.map(({ task, board, statusName, active, done, held }) => {
        const Icon = done ? CheckCircle2 : active ? CircleDot : Circle;
        return (
          <li key={task.id}>
            <button
              type="button"
              data-testid={testId}
              title={`${task.key} · ${task.title} · ${statusName}`}
              onClick={() =>
                void dispatchIntent(
                  "tasks.reveal",
                  {
                    module: "tasks",
                    kind: "task",
                    id: task.id,
                    label: task.key,
                  },
                  { placement: "beside" },
                )
              }
              className="group/task flex h-16 w-full min-w-0 items-center gap-3 rounded-lg px-2.5 text-left transition-control hover:bg-state-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-status-info/30"
            >
              <span
                role="img"
                aria-label={`Status: ${statusName}`}
                title={statusName}
                className="flex size-6 shrink-0 items-center justify-center rounded-full bg-state-soft"
              >
                <Icon
                  aria-hidden
                  className={cn(
                    "size-4 shrink-0",
                    done
                      ? "text-status-success"
                      : active
                        ? "text-status-info"
                        : "text-text-tertiary",
                  )}
                />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-body-13-medium text-text-primary">
                  {task.title}
                </span>
                <span className="mt-1 flex min-w-0 items-center gap-2 text-caption-11 text-text-tertiary">
                  <span className="shrink-0 font-mono">{task.key}</span>
                  <span className="truncate">{board.name}</span>
                  {held ? (
                    <span
                      className="shrink-0 text-status-info"
                      title="Held by this session"
                    >
                      Held
                    </span>
                  ) : null}
                </span>
              </span>
              <ArrowUpRight
                aria-hidden
                className="size-3.5 shrink-0 text-text-tertiary opacity-0 transition-control group-hover/task:opacity-100 group-focus-visible/task:opacity-100"
              />
            </button>
          </li>
        );
      })}
    </ul>
  );
}

type TrayRepo = TrayStatus["repos"][number];

/**
 * Every repository this work spans, one row each: branch, how far it is from
 * its upstream, what is changed. A row opens Git's Changes on that
 * repository (git.reveal), beside the conversation.
 */
export function TrayRepoList({
  repos,
  onClose,
}: {
  repos: TrayRepo[];
  onClose?: () => void;
}) {
  return (
    <section>
      <TrayPanelHeading
        title="Repositories"
        count={repos.length}
        detail="Select a repository to open Git Changes beside this conversation"
        onClose={onClose}
      />
      <ul
        data-testid="tray-repo-list"
        className="max-h-72 space-y-1 overflow-y-auto overscroll-contain p-2 scrollbar-gutter-stable"
      >
        {repos.map((repo) => (
          <li key={repo.path}>
            <button
              type="button"
              data-testid="tray-repo"
              aria-label={`Open ${repo.name} in Git beside this conversation`}
              title={`Open Git Changes · ${repo.error || repo.path}`}
              onClick={() =>
                void dispatchIntent(
                  "git.reveal",
                  {
                    module: "git",
                    kind: "worktree",
                    id: repo.path,
                    label: repo.name,
                  },
                  { placement: "beside" },
                )
              }
              className="flex h-16 w-full min-w-0 items-center gap-3 rounded-lg px-2.5 text-left transition-control hover:bg-state-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-status-info/30"
            >
              <Folder
                className="size-4 shrink-0 text-text-tertiary"
                aria-hidden
              />
              <span className="min-w-0 flex-1">
                <span className="flex min-w-0 items-center gap-2 text-body-13-medium text-text-primary">
                  <span className="truncate">{repo.name}</span>
                  {repo.own ? (
                    <span className="shrink-0 text-caption-11 text-text-tertiary">
                      this session
                    </span>
                  ) : null}
                </span>
                <span className="mt-1 flex min-w-0 items-center gap-2 text-caption-11 text-text-tertiary">
                  <GitBranch className="size-3 shrink-0" aria-hidden />
                  <span className="truncate font-mono">
                    {repoBranch(repo) ?? "—"}
                  </span>
                  {repo.ahead ? (
                    <span className="shrink-0">{`↑${repo.ahead}`}</span>
                  ) : null}
                  {repo.behind ? (
                    <span className="shrink-0">{`↓${repo.behind}`}</span>
                  ) : null}
                </span>
              </span>
              {repo.error || repo.conflicted || repo.changed ? (
                <span
                  className={cn(
                    "shrink-0 rounded-md px-2 py-1 text-caption-11 tabular-nums",
                    repo.error || repo.conflicted
                      ? "bg-status-danger/10 text-status-danger"
                      : repo.changed
                        ? "bg-status-warning/10 text-status-warning"
                        : "bg-state-soft text-text-tertiary",
                  )}
                >
                  {repo.error
                    ? "unreadable"
                    : repo.conflicted
                      ? `${repo.conflicted} conflicted`
                      : repo.changed
                        ? `${repo.changed} changed`
                        : null}
                </span>
              ) : (
                <span
                  role="img"
                  aria-label="No uncommitted changes"
                  title="No uncommitted changes"
                  className="flex size-6 shrink-0 items-center justify-center text-text-tertiary"
                >
                  <CheckCircle2 className="size-4" aria-hidden />
                </span>
              )}
              <ArrowUpRight
                className="size-3.5 shrink-0 text-text-tertiary"
                aria-hidden
              />
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

/** The shell commands still running, each with its own clock. */
function TrayCommandList({
  commands,
  onClose,
}: {
  commands: TranscriptItem[];
  onClose?: () => void;
}) {
  return (
    <section>
      <TrayPanelHeading
        title="Running commands"
        count={commands.length}
        detail="Commands still executing in this session"
        onClose={onClose}
      />
      <ul
        data-testid="tray-command-list"
        className="flex max-h-64 flex-col overflow-y-auto overscroll-contain p-2"
      >
        {commands.map((item) => (
          <TrayCommandRow key={item.id} item={item} />
        ))}
      </ul>
    </section>
  );
}

function TrayCommandRow({ item }: { item: TranscriptItem }) {
  const elapsed = useElapsed(item.createdAt, true);
  const command = commandText(item.toolCall!);
  return (
    <li
      data-testid="tray-command"
      className="flex h-12 min-w-0 items-center gap-3 rounded-lg px-2.5"
      title={command}
    >
      <Terminal
        className="size-3.5 shrink-0 text-icon-secondary"
        aria-hidden="true"
      />
      <code className="min-w-0 flex-1 truncate text-code-12 text-text-primary">
        {command}
      </code>
      <span className="shrink-0 text-caption-11 text-text-tertiary tabular-nums">
        {formatElapsed(elapsed)}
      </span>
    </li>
  );
}
