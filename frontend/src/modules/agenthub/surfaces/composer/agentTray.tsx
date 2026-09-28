import * as React from "react";
import { Bot, Boxes, Folder, GitBranch, ListTodo, MessageCircleQuestion, ShieldAlert } from "lucide-react";

import type { AgentSession, ElicitationResponse, PendingElicitation, PendingPermission } from "@/bridge/agenthub";
import { dispatchIntent } from "@/workbench/intents";
import { cn } from "@/lib/utils";
import { runningSubagents } from "../../turns";
import type { TranscriptItem } from "../../types";
import { AgentPermissionRequest } from "../AgentPermissionRequest";
import { AgentQuestionCard } from "../AgentQuestionCard";
import { AgentSubagentTracker } from "../AgentSubagentTracker";
import { TrayChip, TrayChips, type TrayPanel } from "./ComposerTray";
import { useTrayStatus, type TrayTask } from "./useTrayStatus";

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

type Opened = "subagents" | "tasks" | null;

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
  const running = React.useMemo(() => (live ? runningSubagents(items).running.length : 0), [items, live]);
  const [opened, setOpened] = React.useState<Opened>(null);
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
    opened === "subagents" && running === 0 ? null : opened === "tasks" && !status.hasBoards ? null : opened;

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
      content: <AgentSubagentTracker items={items} live={live} onOpenFile={onOpenFile} variant="tray" />,
    };
  } else if (openedNow === "tasks") {
    panel = { key: "tasks", content: <TrayTaskList tasks={status.tasks} /> };
  }

  const mine = status.tasks.filter((t) => t.mine).length;
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
      {status.branch ? (
        <TrayChip key="branch" testId="tray-branch" icon={<GitBranch />} label={status.branch} title={`Branch ${status.branch}`} />
      ) : null}
      {status.hasBoards ? (
        <TrayChip
          key="tasks"
          testId="tray-tasks"
          icon={<ListTodo />}
          label="tasks"
          count={status.tasks.length}
          title={`${status.tasks.length} open task${status.tasks.length === 1 ? "" : "s"} in this workspace${mine ? ` · ${mine} held by this session` : ""}`}
          active={panel?.key === "tasks"}
          onClick={() => toggle("tasks")}
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
          live
          active={panel?.key === "subagents"}
          onClick={() => toggle("subagents")}
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
 * The workspace's open tasks, this session's first. A row opens the task beside
 * the conversation (tasks.reveal), the way a file mentioned in it does.
 */
function TrayTaskList({ tasks }: { tasks: TrayTask[] }) {
  if (tasks.length === 0) {
    return <p className="flex h-8 items-center px-3 text-body-13 text-text-tertiary">No open tasks on this workspace's boards</p>;
  }
  return (
    <ul data-testid="tray-task-list" className="flex max-h-64 flex-col overflow-y-auto p-1">
      {tasks.map(({ task, statusName, active, mine }) => (
        <li key={task.id}>
          <button
            type="button"
            data-testid="tray-task"
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
            {mine ? (
              <span className="shrink-0 rounded-full bg-state-soft px-1.5 text-caption-11 text-text-secondary">this session</span>
            ) : null}
            <span className={cn("shrink-0 text-caption-11", active ? "text-status-info" : "text-text-tertiary")}>
              {statusName}
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}
