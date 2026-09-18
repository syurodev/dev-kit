import { useState } from "react";
import { TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { classifyIndexError } from "./sidebarModel";

/**
 * Figma `index-error` (State=Index error): why the last index run failed, what
 * that usually means, and three ways on — run it again, read what the analyzer
 * printed, or hand it to the agent.
 */
export function IndexErrorCard({
  error,
  busy,
  onRetry,
  onAskAgent,
}: {
  error: string;
  busy: boolean;
  onRetry: () => void;
  onAskAgent: (text: string) => void;
}) {
  const [logOpen, setLogOpen] = useState(false);
  const failure = classifyIndexError(error);

  return (
    <div role="alert" className="flex shrink-0 flex-col gap-1.5 rounded-lg border border-(--border-danger) bg-(--feedback-danger-soft) p-2.5">
      <div className="flex items-center gap-2">
        <TriangleAlert className="size-4 shrink-0 text-status-danger" aria-hidden />
        <span className="min-w-0 flex-1 truncate text-title-13 text-text-primary">{failure.title}</span>
      </div>
      <p className="text-body-12 text-text-secondary">{failure.body}</p>
      <span className="truncate text-code-11 text-text-tertiary">{failure.code}</span>
      <div className="flex flex-wrap gap-1.5 pt-0.5">
        <Button size="xs" variant="outline" className="bg-surface-raised" onClick={onRetry} disabled={busy}>
          Retry
        </Button>
        <Button size="xs" variant="outline" className="bg-surface-raised" onClick={() => setLogOpen(true)}>
          View log
        </Button>
        <Button
          size="xs"
          variant="outline"
          className="bg-surface-raised"
          onClick={() =>
            onAskAgent(`Code Graph could not index this project: ${failure.title}.\n\n${error}\n\nWhat should I change so it indexes?`)
          }
        >
          Ask agent
        </Button>
      </div>
      <Dialog open={logOpen} onOpenChange={setLogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{failure.title}</DialogTitle>
            <DialogDescription>What the analyzer printed before it stopped.</DialogDescription>
          </DialogHeader>
          <pre className="max-h-96 overflow-auto rounded-md bg-surface-well p-3 text-code-11 whitespace-pre-wrap text-text-secondary">
            {error}
          </pre>
        </DialogContent>
      </Dialog>
    </div>
  );
}
