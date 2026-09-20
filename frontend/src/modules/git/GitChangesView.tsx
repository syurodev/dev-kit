import { useCallback, useEffect, useState } from "react";
import { GitBranch, GitMerge, RefreshCw, ShieldCheck, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Textarea } from "@/components/ui/textarea";
import {
  createGuard,
  deleteGuard,
  gitAvailability,
  gitCommit,
  gitGuards,
  gitStage,
  gitStatus,
  gitUnstage,
  seedMessage,
  type GitStatus,
  type Guard,
} from "@/bridge/git";

function FileList({
  title,
  paths,
  action,
  onAction,
}: {
  title: string;
  paths: string[];
  action: string;
  onAction: (paths: string[]) => void;
}) {
  if (paths.length === 0) {
    return null;
  }
  return (
    <section className="border-b border-border-subtle">
      <header className="flex items-center justify-between px-3 py-1.5">
        <h3 className="text-xs font-medium text-fg-muted">
          {title} ({paths.length})
        </h3>
        <Button variant="ghost" size="sm" onClick={() => onAction(paths)}>
          {action} all
        </Button>
      </header>
      <ul>
        {paths.map((path) => (
          <li key={path} className="flex items-center gap-2 px-3 py-1 text-sm">
            <span className="min-w-0 flex-1 truncate font-mono text-xs">{path}</span>
            <Button variant="ghost" size="sm" onClick={() => onAction([path])}>
              {action}
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}

/**
 * What changed, and turning it into a commit.
 *
 * Conflicted paths are their own section rather than part of "modified":
 * staging one would commit the markers, and the backend refuses a commit while
 * any remain.
 */
export function GitChangesView() {
  const [status, setStatus] = useState<GitStatus | null>(null);
  // null until loaded: "no guard" and "not asked yet" are different answers,
  // and one of them means committing will not ask.
  const [guards, setGuards] = useState<Guard[] | null>(null);
  const [unavailable, setUnavailable] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const reload = useCallback(() => {
    gitStatus()
      .then((next) => {
        setStatus(next);
        setError("");
      })
      .catch((e: Error) => setError(e.message));
    gitGuards()
      .then(setGuards)
      .catch(() => setGuards([]));
  }, []);

  useEffect(() => {
    gitAvailability()
      .then((availability) => {
        if (!availability.ok) {
          setUnavailable(availability.reason || "No usable git was found on this machine.");
          return;
        }
        reload();
      })
      .catch((e: Error) => setUnavailable(e.message));
  }, [reload]);

  if (unavailable) {
    return <EmptyState icon={GitBranch} title="Git is not available" body={unavailable} />;
  }
  if (error && !status) {
    return <EmptyState icon={GitBranch} title="Nothing to show" body={error} />;
  }
  if (!status || !guards) {
    return null;
  }

  // The guard covering the branch you are on, if any. Its absence is the
  // default and is not a problem to report — committing simply does not ask.
  const guard = guards.find((candidate) => matchesBranch(candidate.pattern, status.branch));

  const staged = status.staged ?? [];
  const unstaged = status.unstaged ?? [];
  const untracked = status.untracked ?? [];
  const conflicted = status.conflicted ?? [];
  const clean = staged.length + unstaged.length + untracked.length + conflicted.length === 0;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center gap-2 border-b border-border-subtle px-3 py-2">
        <GitBranch className="size-4 text-fg-muted" aria-hidden />
        <span className="text-sm">{status.branch || "(detached)"}</span>
        {status.ahead > 0 ? (
          <span className="text-xs text-fg-muted">↑{status.ahead}</span>
        ) : null}
        {status.behind > 0 ? (
          <span className="text-xs text-fg-muted">↓{status.behind}</span>
        ) : null}
        {guard ? (
          <span
            className="flex items-center gap-1 text-xs text-amber-500"
            title={`Committing on ${guard.pattern} asks first (${guard.env})`}
          >
            <ShieldCheck className="size-3.5" aria-hidden />
            {guard.env}
          </span>
        ) : null}
        <div className="flex-1" />
        {guard ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              deleteGuard(guard.id).then(reload).catch((e: Error) => setError(e.message));
            }}
          >
            Unprotect
          </Button>
        ) : status.branch ? (
          <Button
            variant="ghost"
            size="sm"
            title="Ask before every commit on this branch"
            onClick={() => {
              // prod is the tag this is nearly always for; an env tag that
              // does not require confirmation makes the guard a no-op, which
              // the backend already treats as "not gated".
              createGuard(status.branch, "prod").then(reload).catch((e: Error) => setError(e.message));
            }}
          >
            Protect branch
          </Button>
        ) : null}
        <Button variant="ghost" size="icon" aria-label="Reload" onClick={reload}>
          <RefreshCw className="size-4" aria-hidden />
        </Button>
      </header>

      <div className="min-h-0 flex-1 overflow-auto">
        {clean ? (
          <EmptyState icon={GitBranch} title="Nothing changed" body="The work tree is clean." />
        ) : null}
        {conflicted.length > 0 ? (
          <section className="border-b border-border-subtle bg-amber-500/5">
            <header className="flex items-center gap-2 px-3 py-1.5">
              <GitMerge className="size-3.5 text-amber-500" aria-hidden />
              <h3 className="text-xs font-medium text-amber-500">
                Conflicted ({conflicted.length}) · resolve before committing
              </h3>
            </header>
            <ul>
              {conflicted.map((path) => (
                <li key={path} className="px-3 py-1 font-mono text-xs">
                  {path}
                </li>
              ))}
            </ul>
          </section>
        ) : null}
        <FileList
          title="Staged"
          paths={staged}
          action="Unstage"
          onAction={(paths) => {
            gitUnstage(paths).then(reload).catch((e: Error) => setError(e.message));
          }}
        />
        <FileList
          title="Changed"
          paths={unstaged}
          action="Stage"
          onAction={(paths) => {
            gitStage(paths).then(reload).catch((e: Error) => setError(e.message));
          }}
        />
        <FileList
          title="Untracked"
          paths={untracked}
          action="Stage"
          onAction={(paths) => {
            gitStage(paths).then(reload).catch((e: Error) => setError(e.message));
          }}
        />
      </div>

      <footer className="border-t border-border-subtle p-3">
        <Textarea
          value={message}
          onChange={(event) => setMessage(event.target.value)}
          placeholder="Commit message"
          rows={3}
          aria-label="Commit message"
        />
        {error ? <p className="pt-1 text-xs text-red-500">{error}</p> : null}
        <div className="flex items-center gap-2 pt-2">
          <Button
            size="sm"
            variant="ghost"
            disabled={staged.length === 0}
            // The seeded body is the list of paths — a statement of fact. The
            // subject line stays the person's to write.
            onClick={() => setMessage((current) => current + seedMessage(staged))}
          >
            Seed from staged
          </Button>
          <div className="flex-1" />
          <Button
            size="sm"
            disabled={staged.length === 0 || message.trim() === "" || conflicted.length > 0}
            onClick={() => {
              gitCommit(message)
                .then(() => {
                  setMessage("");
                  reload();
                })
                .catch((e: Error) => setError(e.message));
            }}
          >
            <Upload className="size-4" aria-hidden />
            Commit
          </Button>
        </div>
      </footer>
    </div>
  );
}

/**
 * Whether a guard's pattern covers a branch. The backend decides for real —
 * this only draws the badge — so it is the same rule and no more: an exact
 * name, or a glob with one `*`.
 */
export function matchesBranch(pattern: string, branch: string): boolean {
  if (!pattern || !branch) return false;
  if (pattern === branch) return true;
  const star = pattern.indexOf("*");
  if (star === -1) return false;
  const head = pattern.slice(0, star);
  const tail = pattern.slice(star + 1);
  return branch.length >= head.length + tail.length && branch.startsWith(head) && branch.endsWith(tail);
}
