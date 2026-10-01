import { useState } from "react";
import { ChevronRight, LoaderCircle } from "lucide-react";
import { toast } from "sonner";
import { cancelVerify, type Run } from "@/bridge/tasks";
import { errorMessage } from "@/bridge/errors";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/ui/chip";
import { queryClient } from "@/lib/query";
import { cn } from "@/lib/utils";
import { taskKeys } from "../taskActions";
import { relativeTime } from "../taskModel";

export function isLive(run: Run): boolean {
  return run.state === "queued" || run.state === "running";
}

function RunRow({ run }: { run: Run }) {
  const [open, setOpen] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const live = isLive(run);

  return (
    <li className="flex flex-col">
      <div className="flex h-8 items-center gap-2">
        <button
          type="button"
          aria-expanded={open}
          aria-label={open ? "Hide output" : "Show output"}
          disabled={!run.output}
          onClick={() => setOpen((value) => !value)}
          className="flex size-5 items-center justify-center rounded-sm text-text-tertiary disabled:opacity-38"
        >
          <ChevronRight
            aria-hidden
            className={cn(
              "size-(--icon-14) transition-transform duration-(--motion-fast)",
              open && "rotate-90",
            )}
          />
        </button>
        <Chip
          tone={run.state === "failed" ? "danger" : "default"}
          className={cn(
            "min-w-20 justify-center",
            run.state === "passed" && "text-status-success",
            live && "text-status-info",
          )}
        >
          {live ? <LoaderCircle aria-hidden className="size-3 animate-spin" /> : null}
          {run.state}
        </Chip>
        <code className="min-w-0 flex-1 truncate text-code-11 text-text-secondary">{run.command}</code>
        {live ? (
          // Cancel stands beside the state rather than in its place, so the
          // row keeps its width while the run is still going (smoothness §5).
          <Button
            size="xs"
            variant="ghost"
            disabled={cancelling}
            onClick={() => {
              setCancelling(true);
              cancelVerify(run.id)
                .then(() => queryClient.invalidateQueries({ queryKey: taskKeys.runs(run.taskId) }))
                .catch((error) => toast.error(errorMessage(error)))
                .finally(() => setCancelling(false));
            }}
          >
            {cancelling ? "Cancelling…" : "Cancel"}
          </Button>
        ) : (
          <span className="shrink-0 text-caption-11 text-text-tertiary tabular-nums">exit {run.exitCode}</span>
        )}
        <span className="w-16 shrink-0 text-right text-caption-11 text-text-tertiary tabular-nums" title={run.requestedAt}>
          {relativeTime(run.requestedAt)}
        </span>
      </div>
      <AnimatedCollapse open={open && !!run.output} className="pb-2 pl-7">
        <pre className="max-h-64 overflow-auto rounded-md border border-border-subtle bg-surface-well p-2 text-code-11 whitespace-pre-wrap text-text-secondary">
          {run.output}
        </pre>
      </AnimatedCollapse>
    </li>
  );
}

/**
 * The verify runs behind this task's closes. DevKit ran each command and its
 * exit code decided — the record of that, not anybody's report of it.
 */
export function TaskRuns({ runs }: { runs: readonly Run[] }) {
  if (runs.length === 0) return null;
  return (
    <section className="flex flex-col gap-1">
      <h3 className="text-title-13 text-text-primary">Verify runs</h3>
      <ul className="flex flex-col">
        {runs.map((run) => (
          <RunRow key={run.id} run={run} />
        ))}
      </ul>
    </section>
  );
}
