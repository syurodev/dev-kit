import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderOpen, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { errorMessage } from "../../bridge/errors";
import {
  chooseConsoleDirectory,
  consoleDirectory,
  setConsoleDirectory,
} from "../../bridge/database";

/**
 * Where query consoles live on disk.
 *
 * A console is a .sql file — one per console, written as you type — and the
 * reason to move them is that a folder has a meaning the app's own data
 * directory does not: a synced folder, a project, an external disk. So the
 * setting is the folder, and the files follow it.
 *
 * The path is shown in full rather than abbreviated to a leaf. It is the
 * whole answer to "where are my consoles", it is what the user pastes into a
 * terminal, and the row has the width for it.
 */
export function ConsoleStorageSetting() {
  const client = useQueryClient();
  const [failed, setFailed] = useState("");

  const { data, error: readError } = useQuery({
    queryKey: ["database", "console-directory"],
    queryFn: consoleDirectory,
  });

  const move = useMutation({
    mutationFn: setConsoleDirectory,
    onSuccess: (directory) => {
      setFailed("");
      client.setQueryData(["database", "console-directory"], directory);
    },
    // The reason a folder was refused is the useful half — "an absolute path",
    // "that is a file" — and it belongs beside the control that refused it.
    onError: (cause) => setFailed(errorMessage(cause)),
  });

  async function pick() {
    try {
      const chosen = await chooseConsoleDirectory();
      // Cancelling is a decision, not a failure: nothing changes and nothing
      // is said about it.
      if (!chosen) return;
      move.mutate(chosen);
    } catch (cause) {
      setFailed(errorMessage(cause));
    }
  }

  const message = failed || (readError ? errorMessage(readError) : "");
  const busy = move.isPending;

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2.5 rounded-control border border-border-subtle bg-surface-raised py-2.5 pr-3 pl-3.5">
        <span className="min-w-0 flex-1 truncate text-code-12-medium text-text-primary" title={data?.path}>
          {data?.path ?? "…"}
        </span>
        {data?.custom ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={() => move.mutate("")}
          >
            <RotateCcw className="size-3.5" strokeWidth={1.5} />
            Use the default
          </Button>
        ) : null}
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => void pick()}
        >
          <FolderOpen className="size-3.5" strokeWidth={1.5} />
          Change
        </Button>
      </div>

      <p className="text-body-12 text-text-tertiary">
        One .sql file per console, written as you type. Changing the folder
        moves the files that are there; a file already in the new folder under
        the same name is left alone.
      </p>

      {message ? <p className="text-body-12 text-status-danger">{message}</p> : null}
    </div>
  );
}
