import { useEffect, useState } from "react";
import { Check, FolderOpen } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { chooseBoardProject, type Board } from "@/bridge/tasks";

/**
 * The command that closes a task on this board, and the folder it runs in.
 *
 * `TasksBoardSetVerify` shipped with no caller, which meant the close gate
 * could never be turned on: an agent could trigger a verify run and nobody
 * could choose what it runs. The gate was designed to be UI-only and
 * tasks:admin exactly so an agent cannot set its own exam — with no UI that
 * made it unreachable rather than protected.
 *
 * An empty command is a real setting, not a blank form: it means closing asks
 * a person every time. That is the default and it is spelled out below,
 * because a form that looks unfinished invites someone to fill it in.
 */
export function BoardVerifyDialog({
  board,
  onClose,
  onSave,
}: {
  board: Board | null;
  onClose: () => void;
  onSave: (command: string, timeoutSec: number, projectPath: string) => void;
}) {
  const [command, setCommand] = useState("");
  const [timeout, setTimeoutSec] = useState("600");
  const [projectPath, setProjectPath] = useState("");

  useEffect(() => {
    if (!board) return;
    setCommand(board.verify?.command ?? "");
    setTimeoutSec(String(board.verify?.timeout_sec || 600));
    setProjectPath(board.projectPath ?? "");
  }, [board]);

  const seconds = Number(timeout);
  const validTimeout = Number.isInteger(seconds) && seconds > 0;

  return (
    <Dialog open={!!board} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[560px]" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>How tasks close on {board?.name}</DialogTitle>
        </DialogHeader>

        <div className="space-y-1.5">
          <label className="text-label-12 text-text-primary" htmlFor="verify-command">
            Verify command
          </label>
          <Input
            id="verify-command"
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            placeholder="go test ./... && bun test"
            spellCheck={false}
            autoFocus
            className="font-mono"
          />
          <p className="text-code-11 text-text-tertiary">
            DevKit runs this and the exit code decides. An agent's own report that the
            tests passed is a sentence with nothing behind it — DevKit cannot see the
            tools it ran. Leave empty and closing asks a person every time.
          </p>
        </div>

        <div className="space-y-1.5">
          <label className="text-label-12 text-text-primary" htmlFor="verify-project">
            Project folder
          </label>
          <div className="flex items-center gap-2">
            <Input
              id="verify-project"
              value={projectPath}
              onChange={(e) => setProjectPath(e.target.value)}
              placeholder="/Users/you/code/billing-service"
              spellCheck={false}
              className="font-mono"
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                // Cancelling hands back "", which must not clear a path the
                // person already chose.
                chooseBoardProject(projectPath)
                  .then((picked) => picked && setProjectPath(picked))
                  .catch(() => undefined);
              }}
            >
              <FolderOpen className="size-4" aria-hidden />
              Choose
            </Button>
          </div>
          <p className="text-code-11 text-text-tertiary">
            Where the command runs. It is also the project a backlog is built from, so
            From plan only appears once this is set.
          </p>
        </div>

        <div className="space-y-1.5">
          <label className="text-label-12 text-text-primary" htmlFor="verify-timeout">
            Timeout (seconds)
          </label>
          <Input
            id="verify-timeout"
            value={timeout}
            onChange={(e) => setTimeoutSec(e.target.value)}
            inputMode="numeric"
            className="font-mono"
          />
          <p className="text-code-11 text-text-tertiary">
            A run that outlives this is cancelled, and a cancelled run is not a pass.
          </p>
        </div>

        <DialogFooter className="sm:justify-between">
          <span className="text-code-11 text-text-tertiary">
            {command.trim()
              ? "A red result refuses the close and does not fall back to asking."
              : "Closing will ask a person every time."}
          </span>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button
              size="sm"
              disabled={!validTimeout}
              onClick={() => onSave(command.trim(), seconds, projectPath.trim())}
            >
              Save
              <Check />
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
