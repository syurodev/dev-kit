import { useCallback, useEffect, useState } from "react";
import { ListChecks } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import {
  getTask,
  listComments,
  splitComments,
  verifyRuns,
  type Comment,
  type Run,
  type Task,
} from "@/bridge/tasks";

/**
 * One task: its body, the conversation, and an Activity tab.
 *
 * System comments are separated rather than interleaved — they are the
 * service's own record of what it ran, not something anybody said.
 */
export function TaskDetailView({ params }: { params?: Record<string, unknown> }) {
  const taskID = typeof params?.taskId === "string" ? params.taskId : "";
  const [task, setTask] = useState<Task | null>(null);
  // null until loaded, so the tab counts do not flash (0) before the
  // first answer arrives.
  const [comments, setComments] = useState<Comment[] | null>(null);
  const [runs, setRuns] = useState<Run[] | null>(null);
  const [tab, setTab] = useState<"conversation" | "activity">("conversation");
  const [error, setError] = useState("");

  const reload = useCallback(() => {
    if (!taskID) {
      return;
    }
    getTask(taskID)
      .then(setTask)
      .catch((e: Error) => setError(e.message));
    listComments(taskID).then(setComments).catch(() => setComments([]));
    verifyRuns(taskID).then(setRuns).catch(() => setRuns([]));
  }, [taskID]);

  useEffect(reload, [reload]);

  if (!taskID) {
    return <EmptyState icon={ListChecks} title="No task open" body="Pick a task on the board." />;
  }
  if (error) {
    return <EmptyState icon={ListChecks} title="Task unavailable" body={error} />;
  }
  if (!task || !comments || !runs) {
    return null;
  }

  const split = splitComments(comments);
  const shown = tab === "conversation" ? split.conversation : split.activity;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="border-b border-border-subtle px-4 py-3">
        <code className="text-xs text-fg-muted">{task.key}</code>
        <h2 className="text-lg">{task.title}</h2>
        <div className="flex gap-3 pt-1 text-xs text-fg-muted">
          <span>{task.status}</span>
          {task.branch ? <span className="font-mono">{task.branch}</span> : null}
          {task.sessionId ? <span>held by an agent</span> : null}
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-auto px-4 py-3">
        {task.description ? (
          <p className="whitespace-pre-wrap text-sm">{task.description}</p>
        ) : null}

        {runs.length > 0 ? (
          <section className="pt-4">
            <h3 className="text-xs font-medium text-fg-muted">Verify runs</h3>
            <ul className="pt-1">
              {runs.map((run) => (
                <li key={run.id} className="py-0.5 text-xs">
                  <span
                    className={
                      run.state === "passed"
                        ? "text-emerald-500"
                        : run.state === "failed"
                          ? "text-red-500"
                          : "text-fg-muted"
                    }
                  >
                    {run.state}
                  </span>{" "}
                  <code>{run.command}</code> · exit {run.exitCode}
                </li>
              ))}
            </ul>
          </section>
        ) : null}

        <nav className="flex gap-3 pt-4 text-xs">
          <button
            className={tab === "conversation" ? "font-medium" : "text-fg-muted"}
            onClick={() => setTab("conversation")}
          >
            Conversation ({split.conversation.length})
          </button>
          <button
            className={tab === "activity" ? "font-medium" : "text-fg-muted"}
            onClick={() => setTab("activity")}
          >
            Activity ({split.activity.length})
          </button>
        </nav>
        <ul className="pt-2">
          {shown.map((comment) => (
            <li key={comment.id} className="border-t border-border-subtle py-2 first:border-t-0">
              <div className="text-xs text-fg-muted">{comment.author}</div>
              <p className="whitespace-pre-wrap text-sm">{comment.body}</p>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
