import { useEffect, useState } from "react";
import { Check } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { type Board } from "@/bridge/tasks";
import { useWorkspaceServices } from "@/stores/workspaceSwitcher";

/**
 * The command that closes a task on this board, and the workspace service it
 * runs in.
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
 *
 * A folder picker used to choose where the command runs; a board syncs and a
 * path does not travel, so it is a service NAME of the board's workspace now
 * — resolved to a path on this machine only when the command actually runs. A
 * board with no binding has no workspace to list services from, so saving a
 * command is disabled until someone links it (Tasks sidebar, "Link to
 * current workspace").
 *
 * Services come from the BOARD's own workspace, not whichever one the
 * title-bar switcher happens to be showing: a command set while looking at
 * one project must still run in the service its own board is bound to, not
 * wherever the switcher was last left.
 */
export function BoardVerifyDialog({
  board,
  onClose,
  onSave,
}: {
  board: Board | null;
  onClose: () => void;
  onSave: (command: string, timeoutSec: number, service: string) => void;
}) {
  const [command, setCommand] = useState("");
  const [timeout, setTimeoutSec] = useState("600");
  const [service, setService] = useState("");
  const { workspace, services } = useWorkspaceServices(board?.workspaceId || null);
  const bound = !!board?.workspaceId;

  useEffect(() => {
    if (!board) return;
    setCommand(board.verify?.command ?? "");
    setTimeoutSec(String(board.verify?.timeout_sec || 600));
    setService(board.verify?.service ?? "");
  }, [board]);

  const seconds = Number(timeout);
  const validTimeout = Number.isInteger(seconds) && seconds > 0;
  const primaryName = services[0]?.name ?? workspace?.name ?? "primary";

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
          <label className="text-label-12 text-text-primary" htmlFor="verify-service">
            Runs in
          </label>
          {bound ? (
            <select
              id="verify-service"
              className="w-full rounded-md border border-border-subtle bg-transparent px-2 py-1.5 text-sm"
              value={service}
              onChange={(e) => setService(e.target.value)}
            >
              <option value="">Primary ({primaryName})</option>
              {services
                .filter((candidate) => candidate.name !== primaryName)
                .map((candidate) => (
                  <option key={candidate.id} value={candidate.name}>
                    {candidate.name}
                  </option>
                ))}
            </select>
          ) : (
            <p className="text-code-11 text-amber-600">
              This board isn't linked to a workspace on this machine. Link it to a
              workspace (Tasks sidebar → Link to current workspace) before setting a command.
            </p>
          )}
          <p className="text-code-11 text-text-tertiary">
            The workspace service the command runs in, resolved to a path on this
            machine each time it runs — a name travels with the board to another
            device where a folder would not.
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
              disabled={!validTimeout || (!bound && !!command.trim())}
              onClick={() => onSave(command.trim(), seconds, bound ? service : "")}
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
