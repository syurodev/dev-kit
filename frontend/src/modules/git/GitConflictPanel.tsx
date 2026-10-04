import { useEffect, useState } from "react";
import { GitMerge, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { MergePanes } from "@/components/ui/merge-panes";
import {
  gitConflict,
  gitResolve,
  gitResolveSide,
  type Conflict,
} from "@/bridge/git";
import { markerCount, readConflict } from "./conflicts";
import { shortcut } from "@/lib/shortcut";

/**
 * Resolving one unmerged path.
 *
 * Until this existed a merge conflict was a dead end in DevKit: the Changes
 * view listed the conflicted paths and refused to commit, and there was no way
 * to fix them without leaving for a terminal.
 *
 * Two things here are not cosmetic. The middle pane starts as **git's own
 * merge** — everything that merged cleanly, plus the markers around what did
 * not — because seeding it from Ours would quietly discard every hunk the
 * other side merged cleanly. And taking a side does not send that side's text
 * back: `gitResolveSide` has git copy its own stage, which keeps line endings,
 * encoding and binary files exactly as they were.
 */
export function GitConflictPanel({
  path,
  branch,
  onClose,
  onResolved,
}: {
  path: string;
  branch: string;
  onClose: () => void;
  onResolved: () => void;
}) {
  const [conflict, setConflict] = useState<Conflict | null>(null);
  const [merged, setMerged] = useState("");
  // The markers git left in the merge as it loaded: the conflicts there were.
  const [total, setTotal] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    setConflict(null);
    setError("");
    gitConflict(path)
      .then((loaded) => {
        if (!active) return;
        setConflict(loaded);
        setMerged(loaded.merged);
        setTotal(markerCount(loaded.merged));
      })
      .catch((e: Error) => active && setError(e.message));
    return () => {
      active = false;
    };
  }, [path]);

  async function run(work: Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await work;
      onResolved();
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  }

  const reading = conflict ? readConflict(conflict, branch) : null;
  const left = markerCount(merged);
  const disabled = busy || !conflict;

  return (
    <div
      className="flex h-full flex-col gap-3 px-6 pt-5 pb-6"
      onKeyDown={(event) => {
        if ((event.metaKey || event.ctrlKey) && event.key === "s" && reading?.mergeable && !disabled) {
          event.preventDefault();
          void run(gitResolve(path, merged));
        }
      }}
    >
      <div className="flex h-9 shrink-0 items-center gap-3">
        <GitMerge className="size-4.5 shrink-0 text-text-secondary" aria-hidden />
        <span className="truncate font-mono text-title-16 text-text-primary">{path}</span>
        <span className="flex h-6 items-center gap-1.5 rounded-md border border-border-subtle bg-surface-raised px-2.5 text-code-11 text-text-secondary">
          <span className="size-1.5 rounded-full bg-status-warning" />
          {/* How many of the file's conflicts are still open — read from the
              merged pane, so it counts down as the person edits. Unknown until
              the merge has loaded, when it says what it can. */}
          {conflict && total > 0 ? `${left} of ${total} conflicts` : "unmerged"}
        </span>
        <span className="flex-1" />
        <Button
          variant="outline"
          className="bg-surface-raised"
          disabled={disabled}
          onClick={() => run(gitResolveSide(path, "ours"))}
        >
          Take ours
        </Button>
        <Button
          variant="outline"
          className="bg-surface-raised"
          disabled={disabled}
          onClick={() => run(gitResolveSide(path, "theirs"))}
        >
          Take theirs
        </Button>
        {reading?.mergeable ? (
          <Button disabled={disabled} onClick={() => run(gitResolve(path, merged))}>
            {/* The label names what the button would actually do. Blocking the
                save instead would dead-end anyone whose file legitimately
                contains a line of angle brackets. */}
            {left > 0 ? "Save with markers" : "Mark resolved"}
            <span className="text-code-11 opacity-65">{shortcut("S")}</span>
          </Button>
        ) : null}
        <span className="h-5 w-px bg-border-subtle" />
        <Button variant="ghost" size="icon-sm" aria-label="Close" onClick={onClose} disabled={busy}>
          <X className="size-3.5" aria-hidden />
        </Button>
      </div>

      <p className="shrink-0 text-body-13 text-text-secondary">
        {error ? (
          <span className="text-status-danger" role="alert">
            {error}
          </span>
        ) : !reading ? (
          "Loading…"
        ) : left > 0 ? (
          <>
            {reading.situation}{" "}
            <span className="text-status-warning">
              {left} conflict marker{left === 1 ? "" : "s"} still in the merged pane.
            </span>
          </>
        ) : (
          reading.situation
        )}
      </p>

      {conflict && reading ? (
        <MergePanes
          disabled={busy}
          left={{ label: reading.oursLabel, text: reading.oursText }}
          right={{ label: reading.theirsLabel, text: reading.theirsText }}
          merged={
            reading.mergeable
              ? { label: "Merged · editable", value: merged, onChange: setMerged }
              : undefined
          }
        />
      ) : null}
    </div>
  );
}
