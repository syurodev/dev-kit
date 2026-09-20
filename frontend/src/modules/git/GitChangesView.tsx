import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  ChevronDown,
  ChevronRight,
  GitBranch,
  GitMerge,
  RefreshCw,
  ShieldCheck,
  Upload,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { EmptyState } from "@/components/ui/empty-state";
import { Textarea } from "@/components/ui/textarea";
import {
  createGuard,
  deleteGuard,
  gitAbortMerge,
  gitAvailability,
  gitCommit,
  gitFilePatch,
  gitGuards,
  gitStage,
  gitStageHunk,
  gitStatus,
  gitUnstage,
  seedMessage,
  type GitStatus,
  type Guard,
} from "@/bridge/git";
import { buildPatch, hunkSummary, parsePatch, type FilePatch } from "./hunks";
import { GitConflictPanel } from "./GitConflictPanel";
import { bumpGitRevision } from "./gitRevision";

function FileList({
  title,
  paths,
  action,
  onAction,
  expanded,
  onExpand,
  renderExpansion,
}: {
  title: string;
  paths: string[];
  action: string;
  onAction: (paths: string[]) => void;
  /** The one path whose disclosure is open. Only one is, deliberately. */
  expanded?: string;
  onExpand?: (path: string) => void;
  renderExpansion?: (path: string) => ReactNode;
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
          <li key={path} className="text-sm">
            <div className="flex items-center gap-2 px-3 py-1">
              {onExpand ? (
                <button
                  type="button"
                  className="text-fg-muted hover:text-text-primary"
                  aria-expanded={expanded === path}
                  aria-label={expanded === path ? `Hide hunks in ${path}` : `Show hunks in ${path}`}
                  onClick={() => onExpand(expanded === path ? "" : path)}
                >
                  {expanded === path ? (
                    <ChevronDown className="size-3.5" aria-hidden />
                  ) : (
                    <ChevronRight className="size-3.5" aria-hidden />
                  )}
                </button>
              ) : null}
              <span className="min-w-0 flex-1 truncate font-mono text-xs">{path}</span>
              <Button variant="ghost" size="sm" onClick={() => onAction([path])}>
                {action}
              </Button>
            </div>
            {expanded === path && renderExpansion ? renderExpansion(path) : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

/** Long hunks are shown to their first this-many lines and then counted. */
const PREVIEW_LINES = 40;

function hunkLineClass(line: string): string {
  if (line.startsWith("+")) return "text-emerald-500";
  if (line.startsWith("-")) return "text-red-500";
  // A "\ No newline at end of file" line is git talking, not the file.
  if (line.startsWith("\\")) return "text-fg-muted italic";
  return "";
}

/**
 * Picking hunks out of one file and staging only those.
 *
 * The patch is re-read after every staging rather than adjusted in place: the
 * line numbers in the hunks that remain are measured against the index, and
 * staging one hunk moves the index. Asking git again is cheap and cannot drift.
 */
function HunkPicker({
  path,
  onStaged,
  onError,
}: {
  path: string;
  onStaged: () => void;
  onError: (message: string) => void;
}) {
  const [patch, setPatch] = useState<FilePatch | null>(null);
  // A Set, not an array of indexes, because ticking is the only thing that
  // happens to it and order never matters — buildPatch sorts what it is given.
  const [picked, setPicked] = useState<ReadonlySet<number>>(() => new Set<number>());
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    gitFilePatch(path)
      .then((text) => {
        setPatch(parsePatch(text));
        setPicked(new Set<number>());
      })
      .catch((e: Error) => onError(e.message));
  }, [path, onError]);

  useEffect(load, [load]);

  if (!patch) {
    return <p className="px-3 pb-2 pl-8 text-xs text-fg-muted">Reading the diff…</p>;
  }
  if (patch.binary || patch.hunks.length === 0) {
    return (
      <p className="px-3 pb-2 pl-8 text-xs text-fg-muted">
        {patch.binary
          ? "Binary file — there is nothing to pick apart. Stage the whole file or none of it."
          : "Git reports no hunks here. Stage the whole file or none of it."}
      </p>
    );
  }

  const all = picked.size === patch.hunks.length;
  const toggle = (index: number) => {
    setPicked((current) => {
      const next = new Set(current);
      if (!next.delete(index)) next.add(index);
      return next;
    });
  };

  return (
    <div className="border-y border-border-subtle bg-white/2">
      <header className="flex items-center gap-2 py-1 pr-2 pl-8">
        <span className="flex-1 text-xs text-fg-muted">
          {patch.hunks.length} hunk{patch.hunks.length === 1 ? "" : "s"}
        </span>
        <Button
          variant="ghost"
          size="sm"
          onClick={() =>
            setPicked(all ? new Set<number>() : new Set(patch.hunks.map((_, index) => index)))
          }
        >
          {all ? "None" : "All"}
        </Button>
        <Button
          size="sm"
          disabled={picked.size === 0 || busy}
          onClick={() => {
            const text = buildPatch(patch, [...picked]);
            if (!text) return;
            setBusy(true);
            gitStageHunk(text)
              .then(() => {
                // Both: the file lists change, and so does what is left here.
                onStaged();
                load();
              })
              .catch((e: Error) => onError(e.message))
              .finally(() => setBusy(false));
          }}
        >
          Stage {picked.size} hunk{picked.size === 1 ? "" : "s"}
        </Button>
      </header>
      <ul>
        {patch.hunks.map((hunk, index) => (
          <li key={`${index}:${hunk.header}`} className="border-t border-border-subtle">
            <label className="flex cursor-pointer items-center gap-2 py-1 pr-3 pl-8">
              <Checkbox
                checked={picked.has(index)}
                onCheckedChange={() => toggle(index)}
                aria-label={`Stage ${hunkSummary(hunk)}`}
              />
              <span className="text-xs text-fg-muted">{hunkSummary(hunk)}</span>
            </label>
            <pre className="overflow-x-auto pr-3 pb-1.5 pl-8 font-mono text-xs leading-snug">
              {hunk.lines.slice(0, PREVIEW_LINES).map((line, lineIndex) => (
                <div key={lineIndex} className={hunkLineClass(line)}>
                  {line.replace(/\n$/, "") || " "}
                </div>
              ))}
              {hunk.lines.length > PREVIEW_LINES ? (
                <div className="text-fg-muted">
                  …{hunk.lines.length - PREVIEW_LINES} more lines
                </div>
              ) : null}
            </pre>
          </li>
        ))}
      </ul>
    </div>
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
  // The one changed file whose hunks are open. Empty is none.
  const [expanded, setExpanded] = useState("");
  // The unmerged path being resolved. The panel takes the whole view while it
  // is open: a three-pane merge does not fit beside a file list, and nothing
  // else here can be done until the merge is finished or thrown away.
  const [resolving, setResolving] = useState("");
  const [error, setError] = useState("");

  const reload = useCallback(() => {
    // Whoever calls this has just changed the repository or is about to look at
    // it; the rail beside the view reads it again either way.
    bumpGitRevision();
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

  if (resolving) {
    return (
      <GitConflictPanel
        path={resolving}
        branch={status.branch}
        onClose={() => setResolving("")}
        onResolved={() => {
          setResolving("");
          reload();
        }}
      />
    );
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
              <h3 className="flex-1 text-xs font-medium text-amber-500">
                Conflicted ({conflicted.length}) · resolve before committing
              </h3>
              <Button
                variant="ghost"
                size="sm"
                title="Throw the whole merge away and go back to before it started"
                onClick={() => {
                  gitAbortMerge().then(reload).catch((e: Error) => setError(e.message));
                }}
              >
                Abort merge
              </Button>
            </header>
            <ul>
              {conflicted.map((path) => (
                <li key={path} className="flex items-center gap-2 px-3 py-1">
                  <span className="min-w-0 flex-1 truncate font-mono text-xs">{path}</span>
                  <Button variant="ghost" size="sm" onClick={() => setResolving(path)}>
                    Resolve
                  </Button>
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
          expanded={expanded}
          onExpand={setExpanded}
          // Untracked files are not expandable: git does not diff them, so
          // there would be no hunks to pick.
          renderExpansion={(path) => (
            <HunkPicker path={path} onStaged={reload} onError={setError} />
          )}
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
