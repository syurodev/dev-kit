import * as React from "react";
import { Bot, Boxes, Eye, Folder, GitBranch, ListTodo, MessageCircleQuestion, ShieldAlert, Terminal } from "lucide-react";

import type { AgentSession, ElicitationResponse, PendingElicitation, PendingPermission } from "@/bridge/agenthub";
import { dispatchIntent } from "@/workbench/intents";
import { cn } from "@/lib/utils";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { sessionRunningSubagents } from "../../subagents";
import type { TranscriptItem } from "../../types";
import { AgentPermissionRequest } from "../AgentPermissionRequest";
import { AgentQuestionCard } from "../AgentQuestionCard";
import { AgentSubagentTracker } from "../AgentSubagentTracker";
import { TrayChip, TrayChips, type TrayPanel } from "./ComposerTray";
import { SLOW_COMMAND_MS, commandText, formatElapsed, isRunningCommand, useElapsed } from "../../transcript/AgentRunningCommand";
import { repoBranch, useTrayStatus, type TrayStatus, type TrayTask } from "./useTrayStatus";
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

type Opened = "subagents" | "tasks" | "repos" | "commands" | null;

function folderName(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  return trimmed.slice(Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\")) + 1) || trimmed;
}

/**
 * Everything the composer tray shows for one conversation (see ComposerTray):
 * the chips of its strip and the panel above them, with the rule for which
 * panel wins.
 *
 * One panel at a time, first of:
 * 1. a permission the agent is waiting on,
 * 2. a question it is waiting on,
 * 3. whatever the person opened from a chip (subagents, tasks).
 * (An "@" or "/" list being typed goes above all of these — the composer
 * decides that, since it is the one that knows.)
 *
 * A permission or question can be folded into the strip to read the transcript
 * that explains it: folding is remembered per request, so a new request opens
 * on its own, and the folded one stays in the strip as a pulsing chip that
 * opens it again — the agent is still waiting on it.
 */
export function useAgentTray(input: AgentTrayInput): { strip: React.ReactNode; panel: TrayPanel | null } {
  const { size, session, projectPath, live, items, onOpenFile, leading, permission, question } = input;
  const status = useTrayStatus(projectPath, session?.id ?? "", `${session?.id}:${live}`);
  // Running by the tracked state, not only while the turn runs: a lead agent
  // that ends its turn to wait on background subagents used to make the chip
  // vanish while they were still working (and the sidebar said so).
  const tracked = useAgentSessionsStore((s) => (session ? s.subagents[session.id] : undefined));
  const running = React.useMemo(() => sessionRunningSubagents(items, tracked, live).length, [items, tracked, live]);
  // Shell commands still running, oldest first — the one the clock reads.
  const commands = React.useMemo(() => (live ? items.filter(isRunningCommand) : []), [items, live]);
  const commandsFor = useElapsed(commands[0]?.createdAt, commands.length > 0);
  const slowCommands = commands.length > 0 && commandsFor >= SLOW_COMMAND_MS;
  const [opened, setOpened] = React.useState<Opened>(null);
  const [following, toggleFollowing] = useFollowing(session?.id);
  const [folded, setFolded] = React.useState<ReadonlySet<string>>(() => new Set());

  const fold = (id: string) => setFolded((prev) => new Set(prev).add(id));
  const unfold = (id: string) =>
    setFolded((prev) => {
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  const toggle = (panel: Exclude<Opened, null>) => setOpened((current) => (current === panel ? null : panel));

  const permissionOpen = permission && !folded.has(permission.pending.id) ? permission : null;
  const questionOpen = question && !folded.has(question.pending.id) ? question : null;
  // A panel opened from a chip whose subject has gone (the last subagent
  // finished) closes with it rather than showing an empty list.
  const openedNow: Opened =
    opened === "subagents" && running === 0
      ? null
      : opened === "tasks" && !status.hasBoards
        ? null
        : opened === "repos" && status.repos.length === 0
          ? null
          : opened === "commands" && !slowCommands
            ? null
            : opened;

  let panel: TrayPanel | null = null;
  if (permissionOpen) {
    panel = {
      key: `permission:${permissionOpen.pending.id}`,
      content: (
        <div className="max-h-screen-60 overflow-y-auto">
          <AgentPermissionRequest
            pending={permissionOpen.pending}
            session={session}
            onResolve={permissionOpen.onResolve}
            busy={permissionOpen.busy}
            size={size}
            onMinimize={() => fold(permissionOpen.pending.id)}
          />
        </div>
      ),
    };
  } else if (questionOpen) {
    panel = {
      key: `question:${questionOpen.pending.id}`,
      content: (
        <div className="max-h-screen-60 overflow-y-auto">
          <AgentQuestionCard
            pending={questionOpen.pending}
            session={session}
            onRespond={questionOpen.onRespond}
            busy={questionOpen.busy}
            error={questionOpen.error}
            size={size}
            onMinimize={() => fold(questionOpen.pending.id)}
          />
        </div>
      ),
    };
  } else if (openedNow === "subagents") {
    panel = {
      key: "subagents",
      content: <AgentSubagentTracker items={items} live={live} tracked={tracked} onOpenFile={onOpenFile} variant="tray" />,
    };
  } else if (openedNow === "commands") {
    panel = { key: "commands", content: <TrayCommandList commands={commands} /> };
  } else if (openedNow === "repos") {
    panel = { key: "repos", content: <TrayRepoList repos={status.repos} /> };
  } else if (openedNow === "tasks") {
    panel = { key: "tasks", content: <TrayTaskList tasks={status.tasks} others={status.others} session={Boolean(session)} /> };
  }

  const strip = (
    <TrayChips>
      {leading ? (
        <span key="where" className="flex min-w-0 shrink">
          {leading}
        </span>
      ) : (
        <TrayChip
          key="where"
          testId="tray-workspace"
          icon={status.workspace ? <Boxes /> : <Folder />}
          label={
            status.workspace
              ? `${status.workspace.name} (${status.workspace.folders})`
              : projectPath
                ? folderName(projectPath)
                : "No project"
          }
          title={projectPath || undefined}
        />
      )}
      {status.repos.length > 0 ? (
        <TrayChip
          key="branch"
          testId="tray-branch"
          icon={<GitBranch />}
          // One repository: its branch, as it always was. Several (a workspace
          // of services): how many, with what needs attention — the single
          // branch said nothing about any of them.
          label={status.repos.length === 1 ? (status.branch ?? status.repos[0].name) : `${status.repos.length} repos`}
          count={repoAttention(status.repos) || undefined}
          title={repoTitle(status.repos, status.branch)}
          active={panel?.key === "repos"}
          onClick={() => toggle("repos")}
        />
      ) : null}
      {status.hasBoards ? (
        <TrayChip
          key="tasks"
          testId="tray-tasks"
          icon={<ListTodo />}
          label="tasks"
          // This session's tasks, not the workspace's: every session of a
          // workspace used to show the same number.
          count={session ? status.tasks.length : status.others.length}
          title={
            session
              ? `${status.tasks.length} task${status.tasks.length === 1 ? "" : "s"} from this session · ${status.others.length} other open in the workspace`
              : `${status.others.length} open task${status.others.length === 1 ? "" : "s"} in this workspace`
          }
          active={panel?.key === "tasks"}
          onClick={() => toggle("tasks")}
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
      {/* Follow: what this agent changes in DevKit shows as it happens — the
          activity panel in Agent Hub, a toast anywhere else (follow.tsx). A
          session that does not exist yet (a draft) has nothing to follow. */}
      {session ? (
        <TrayChip
          key="follow"
          testId="tray-follow"
          icon={<Eye />}
          label={following ? "Following" : "Follow"}
          title={following ? "Stop following this agent" : "Follow what this agent changes in DevKit as it happens"}
          active={following}
          onClick={toggleFollowing}
        />
      ) : null}
      {/* Waiting and not on screen — folded away, or behind something else. */}
      {permission && panel?.key !== `permission:${permission.pending.id}` ? (
        <TrayChip
          key="permission"
          testId="tray-permission"
          icon={<ShieldAlert />}
          label="Permission"
          title="The agent is waiting for a permission — open it"
          tone="attention"
          onClick={() => {
            unfold(permission.pending.id);
            setOpened(null);
          }}
        />
      ) : null}
      {question && panel?.key !== `question:${question.pending.id}` ? (
        <TrayChip
          key="question"
          testId="tray-question"
          icon={<MessageCircleQuestion />}
          label="Question"
          title="The agent is waiting on a question — open it"
          tone="attention"
          onClick={() => {
            unfold(question.pending.id);
            setOpened(null);
            // Behind a permission, the question only comes forward once that
            // is folded: the permission is the one blocking the agent.
            if (permission) fold(permission.pending.id);
          }}
        />
      ) : null}
    </TrayChips>
  );

  return { strip, panel };
}

/**
 * This session's tasks, then — apart, under their own label — the rest of the
 * workspace's open ones. A row opens the task beside the conversation
 * (tasks.reveal), the way a file mentioned in it does.
 */
export function TrayTaskList({ tasks, others, session }: { tasks: TrayTask[]; others: TrayTask[]; session: boolean }) {
  if (tasks.length === 0 && others.length === 0) {
    return <p className="flex h-8 items-center px-3 text-body-13 text-text-tertiary">No open tasks on this workspace's boards</p>;
  }
  return (
    <div data-testid="tray-task-list" className="flex max-h-64 flex-col overflow-y-auto p-1">
      {session ? (
        <>
          <TrayTaskHeading>This session</TrayTaskHeading>
          {tasks.length > 0 ? (
            <TrayTaskRows tasks={tasks} testId="tray-task" />
          ) : (
            <p className="flex h-7 items-center px-2 text-body-13 text-text-tertiary">No tasks from this session yet</p>
          )}
        </>
      ) : null}
      {others.length > 0 ? (
        <>
          {session ? <TrayTaskHeading>Open in the workspace</TrayTaskHeading> : null}
          <TrayTaskRows tasks={others} testId="tray-task-other" />
        </>
      ) : null}
    </div>
  );
}

function TrayTaskHeading({ children }: { children: React.ReactNode }) {
  return <p className="px-2 pt-1.5 pb-0.5 text-caption-11-medium text-text-tertiary">{children}</p>;
}

function TrayTaskRows({ tasks, testId }: { tasks: TrayTask[]; testId: string }) {
  return (
    <ul className="flex flex-col">
      {tasks.map(({ task, statusName, active, done, held }) => (
        <li key={task.id}>
          <button
            type="button"
            data-testid={testId}
            onClick={() =>
              void dispatchIntent(
                "tasks.reveal",
                { module: "tasks", kind: "task", id: task.id, label: task.key },
                { placement: "beside" },
              )
            }
            className="flex h-7 w-full min-w-0 cursor-pointer items-center gap-2 rounded-sm px-2 text-left text-body-13 hover:bg-state-soft"
          >
            <span className="shrink-0 font-mono text-code-11 text-text-tertiary">{task.key}</span>
            <span className="min-w-0 flex-1 truncate text-text-primary">{task.title}</span>
            {held ? (
              <span className="shrink-0 rounded-full bg-state-soft px-1.5 text-caption-11 text-text-secondary">held</span>
            ) : null}
            <span
              className={cn(
                "shrink-0 text-caption-11",
                active ? "text-status-info" : done ? "text-status-success" : "text-text-tertiary",
              )}
            >
              {statusName}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}

type TrayRepo = TrayStatus["repos"][number];

/** Repositories with something to look at: changes or conflicts. */
function repoAttention(repos: TrayRepo[]): number {
  return repos.filter((repo) => repo.changed > 0 || repo.conflicted > 0).length;
}

function repoTitle(repos: TrayRepo[], branch: string | null): string {
  if (repos.length === 1) return branch ? `Branch ${branch}` : repos[0].name;
  const dirty = repoAttention(repos);
  return `${repos.length} repositories${dirty ? ` · ${dirty} with changes` : ""}`;
}

/**
 * Every repository this work spans, one row each: branch, how far it is from
 * its upstream, what is changed. A row opens Git's Changes on that
 * repository (git.reveal), beside the conversation.
 */
export function TrayRepoList({ repos }: { repos: TrayRepo[] }) {
  return (
    <ul data-testid="tray-repo-list" className="flex max-h-64 flex-col overflow-y-auto p-1">
      {repos.map((repo) => (
        <li key={repo.path}>
          <button
            type="button"
            data-testid="tray-repo"
            title={repo.error || repo.path}
            onClick={() =>
              void dispatchIntent(
                "git.reveal",
                { module: "git", kind: "worktree", id: repo.path, label: repo.name },
                { placement: "beside" },
              )
            }
            className="flex h-7 w-full min-w-0 cursor-pointer items-center gap-2 rounded-sm px-2 text-left text-body-13 hover:bg-state-soft"
          >
            <span className="min-w-0 shrink truncate text-text-primary">{repo.name}</span>
            {repo.own ? (
              <span className="shrink-0 rounded-full bg-state-soft px-1.5 text-caption-11 text-text-secondary">this session</span>
            ) : null}
            <span className="ml-auto flex shrink-0 items-center gap-2 text-caption-11 tabular-nums">
              {repo.error ? (
                <span className="text-status-danger">unreadable</span>
              ) : (
                <>
                  <span className="flex items-center gap-1 font-mono text-code-11 text-text-secondary">
                    <GitBranch className="size-3" aria-hidden="true" />
                    {repoBranch(repo) ?? "—"}
                  </span>
                  {repo.ahead ? <span className="text-text-tertiary">{`↑${repo.ahead}`}</span> : null}
                  {repo.behind ? <span className="text-text-tertiary">{`↓${repo.behind}`}</span> : null}
                  {repo.conflicted ? (
                    <span className="text-status-danger">{`${repo.conflicted} conflicted`}</span>
                  ) : repo.changed ? (
                    <span className="text-status-warning">{`${repo.changed} changed`}</span>
                  ) : (
                    <span className="text-text-tertiary">clean</span>
                  )}
                </>
              )}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}

/** The shell commands still running, each with its own clock. */
function TrayCommandList({ commands }: { commands: TranscriptItem[] }) {
  return (
    <ul data-testid="tray-command-list" className="flex max-h-64 flex-col overflow-y-auto p-1">
      {commands.map((item) => (
        <TrayCommandRow key={item.id} item={item} />
      ))}
    </ul>
  );
}

function TrayCommandRow({ item }: { item: TranscriptItem }) {
  const elapsed = useElapsed(item.createdAt, true);
  const command = commandText(item.toolCall!);
  return (
    <li data-testid="tray-command" className="flex h-7 min-w-0 items-center gap-2 px-2" title={command}>
      <Terminal className="size-3.5 shrink-0 text-icon-secondary" aria-hidden="true" />
      <code className="min-w-0 flex-1 truncate text-code-12 text-text-primary">{command}</code>
      <span className="shrink-0 text-caption-11 text-text-tertiary tabular-nums">{formatElapsed(elapsed)}</span>
    </li>
  );
}
