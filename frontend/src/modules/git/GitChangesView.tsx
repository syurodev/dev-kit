import { useCallback, useEffect, useState, type ReactNode } from "react";
import { ArrowRight, ChevronDown, ChevronRight, GitBranch, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DiffLineRow } from "@/components/ui/diff";
import { EmptyState } from "@/components/ui/empty-state";
import { Textarea } from "@/components/ui/textarea";
import {
  createGuard,
  deleteGuard,
  gitAbortMerge,
  gitAvailability,
  gitCommit,
  gitFetch,
  gitFilePatch,
  gitGuards,
  gitPull,
  gitPush,
  gitRemotes,
  gitStage,
  gitStageHunk,
  gitStatus,
  gitUnstage,
  seedMessage,
  type GitStatus,
  type Guard,
  type Remote,
} from "@/bridge/git";
import { cn } from "@/lib/utils";
import { buildPatch, parsePatch, type FilePatch, type Hunk } from "./hunks";
import { GitConflictPanel } from "./GitConflictPanel";
import { GitFolderBar } from "./GitFolderGate";
import { HeadingAction, RowAction, StatusMark } from "./GitParts";
import { focusGitPath, useGitFocus } from "./gitFocus";
import { bumpGitRevision, useGitRevision } from "./gitRevision";
import { markKind, splitPath, syncText, type MarkKind } from "./rail";
import { remoteFor } from "./remotes";

/** Long hunks are shown to their first this-many lines and then counted. */
const PREVIEW_LINES = 40;

/** A hunk line is its diff prefix and then the text; the shared Diff / Line draws the prefix itself. */
function hunkRow(line: string): { kind: "same" | "added" | "removed"; text: string } {
  const text = line.replace(/\n$/, "");
  if (line.startsWith("+")) return { kind: "added", text: text.slice(1) };
  if (line.startsWith("-")) return { kind: "removed", text: text.slice(1) };
  // A "\ No newline at end of file" line is git talking, not the file: it is
  // kept whole so it reads as what it is.
  if (line.startsWith("\\")) return { kind: "same", text };
  return { kind: "same", text: text.slice(1) };
}

/**
 * The hunks of one changed file, each with its own button to stage just that
 * one (Figma `Git / Hunk`).
 *
 * The patch is re-read after every staging rather than adjusted in place: the
 * line numbers in the hunks that remain are measured against the index, and
 * staging one hunk moves the index. Asking git again is cheap and cannot drift.
 */
function HunkList({
  path,
  onStaged,
  onError,
}: {
  path: string;
  onStaged: () => void;
  onError: (message: string) => void;
}) {
  const [patch, setPatch] = useState<FilePatch | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    gitFilePatch(path)
      .then((text) => setPatch(parsePatch(text)))
      .catch((e: Error) => onError(e.message));
  }, [path, onError]);

  useEffect(load, [load]);

  if (!patch) {
    return <p className="px-3 pb-2 pl-10 text-caption-11 text-text-tertiary">Reading the diff…</p>;
  }
  if (patch.binary || patch.hunks.length === 0) {
    return (
      <p className="px-3 pb-2 pl-10 text-caption-11 text-text-tertiary">
        {patch.binary
          ? "Binary file — there is nothing to pick apart. Stage the whole file or none of it."
          : "Git reports no hunks here. Stage the whole file or none of it."}
      </p>
    );
  }

  function stage(index: number) {
    if (!patch) return;
    const text = buildPatch(patch, [index]);
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
  }

  return (
    <div className="flex flex-col gap-2 px-3 pb-3">
      {patch.hunks.map((hunk, index) => (
        <HunkCard key={`${index}:${hunk.header}`} hunk={hunk} busy={busy} onStage={() => stage(index)} />
      ))}
    </div>
  );
}

function HunkCard({ hunk, busy, onStage }: { hunk: Hunk; busy: boolean; onStage: () => void }) {
  return (
    <div className="overflow-clip rounded-lg border border-border-subtle bg-surface-well">
      <div className="flex items-center gap-2 py-1.5 pr-2 pl-3">
        <span className="min-w-0 flex-1 truncate text-code-11 text-text-tertiary" title={hunk.header}>
          {hunk.header}
        </span>
        <RowAction disabled={busy} onClick={onStage}>
          Stage hunk
        </RowAction>
      </div>
      <div className="overflow-x-auto pb-1">
        {hunk.lines.slice(0, PREVIEW_LINES).map((line, index) => (
          <DiffLineRow key={index} row={hunkRow(line)} />
        ))}
        {hunk.lines.length > PREVIEW_LINES ? (
          <div className="px-3 py-1 text-code-11 text-text-tertiary">
            …{hunk.lines.length - PREVIEW_LINES} more lines
          </div>
        ) : null}
      </div>
    </div>
  );
}

/** One list of files under a heading, in a card — Staged, Changes, Untracked. */
function ChangeSection({
  title,
  count,
  tone = "default",
  actions,
  children,
}: {
  title: string;
  count: number;
  tone?: "default" | "warning";
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section>
      <header className="flex items-center justify-between gap-2 px-1 pb-1.5">
        <h3
          className={cn(
            "flex items-baseline gap-1.5 text-label-11-caps",
            tone === "warning" ? "text-status-warning" : "text-text-tertiary",
          )}
        >
          {title}
          <span className="text-code-11 opacity-70">{count}</span>
        </h3>
        {actions ? <div className="flex items-center gap-3">{actions}</div> : null}
      </header>
      <ul className="flex flex-col rounded-lg border border-border-subtle bg-surface-well p-1">{children}</ul>
    </section>
  );
}

/**
 * Figma `Git / Change Row`. Only a changed file can unfold: git does not diff
 * an untracked file and a staged one has no hunks left to pick, so their rows
 * keep the space where the chevron would be and stay aligned with the others.
 */
function ChangeRow({
  path,
  kind,
  action,
  onAction,
  open,
  onToggle,
  children,
}: {
  path: string;
  kind: MarkKind;
  action: string;
  onAction: () => void;
  /** Undefined for a row that cannot unfold. */
  open?: boolean;
  onToggle?: () => void;
  children?: ReactNode;
}) {
  const { name, dir } = splitPath(path);
  const unfolded = open === true;
  const label = (
    <>
      {onToggle ? (
        unfolded ? (
          <ChevronDown className="size-(--icon-12) shrink-0 text-text-tertiary" aria-hidden />
        ) : (
          <ChevronRight className="size-(--icon-12) shrink-0 text-text-tertiary" aria-hidden />
        )
      ) : (
        <span className="size-(--icon-12) shrink-0" aria-hidden />
      )}
      <StatusMark kind={kind} />
      <span className="min-w-0 truncate text-code-12 text-text-primary">{name}</span>
      {dir ? <span className="min-w-0 truncate text-code-11 text-text-tertiary">{dir}</span> : null}
    </>
  );
  return (
    <li className={cn("rounded-md", unfolded && "bg-state-soft")}>
      <div className="flex items-center gap-2.5 py-1.5 pr-3 pl-3">
        {onToggle ? (
          <button
            type="button"
            aria-expanded={unfolded}
            aria-label={unfolded ? `Hide hunks in ${path}` : `Show hunks in ${path}`}
            title={path}
            onClick={onToggle}
            className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
          >
            {label}
          </button>
        ) : (
          <div className="flex min-w-0 flex-1 items-center gap-2.5" title={path}>
            {label}
          </div>
        )}
        <RowAction onClick={onAction}>{action}</RowAction>
      </div>
      {unfolded ? children : null}
    </li>
  );
}

/**
 * What changed, and turning it into a commit.
 *
 * Conflicted paths are their own section rather than part of "modified":
 * staging one would commit the markers, and the backend refuses a commit while
 * any remain.
 *
 * Every change made here — and by the rail, which stages too — goes through
 * bumpGitRevision(), and this view reads again when the revision moves. There
 * is no other way it learns the repository changed underneath it.
 */
export function GitChangesView() {
  const revision = useGitRevision((state) => state.revision);
  const expanded = useGitFocus((state) => state.path);
  const [status, setStatus] = useState<GitStatus | null>(null);
  // null until loaded: "no guard" and "not asked yet" are different answers,
  // and one of them means committing will not ask.
  const [guards, setGuards] = useState<Guard[] | null>(null);
  // null until asked: fetch, pull and push stay disabled until then, and no
  // remote is the answer only when git has said so.
  const [remotes, setRemotes] = useState<Remote[] | null>(null);
  const [unavailable, setUnavailable] = useState("");
  const [ready, setReady] = useState(false);
  const [message, setMessage] = useState("");
  // The unmerged path being resolved. The panel takes the whole view while it
  // is open: a three-pane merge does not fit beside a file list, and nothing
  // else here can be done until the merge is finished or thrown away.
  const [resolving, setResolving] = useState("");
  // The network action in flight, "" when none. One at a time: a pull under a
  // push is a race over the same branch.
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    gitAvailability()
      .then((availability) => {
        if (!availability.ok) {
          setUnavailable(availability.reason || "No usable git was found on this machine.");
          return;
        }
        setReady(true);
      })
      .catch((e: Error) => setUnavailable(e.message));
  }, []);

  useEffect(() => {
    if (!ready) return;
    let live = true;
    gitStatus()
      .then((next) => {
        if (!live) return;
        setStatus(next);
        setError("");
      })
      .catch((e: Error) => live && setError(e.message));
    gitGuards()
      .then((next) => live && setGuards(next))
      .catch(() => live && setGuards([]));
    gitRemotes()
      .then((next) => live && setRemotes(next))
      .catch(() => live && setRemotes([]));
    return () => {
      live = false;
    };
    // revision is not read: it is the reason to read again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ready, revision]);

  const fail = useCallback((e: Error) => setError(e.message), []);

  /** Runs a change to the repository, then has everything that shows it read again. */
  function change(work: Promise<unknown>) {
    work.then(bumpGitRevision).catch(fail);
  }

  function network(key: string, work: Promise<void>, done: string) {
    setBusy(key);
    setNotice("");
    setError("");
    work
      .then(() => {
        setNotice(done);
        bumpGitRevision();
      })
      .catch(fail)
      .finally(() => setBusy(""));
  }

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
          bumpGitRevision();
        }}
      />
    );
  }

  // The guard covering the branch you are on, if any. Its absence is the
  // default and is not a problem to report — committing simply does not ask.
  const guard = guards.find((candidate) => matchesBranch(candidate.pattern, status.branch));
  const remote = remoteFor(
    status.upstream,
    (remotes ?? []).map((r) => r.name),
  );

  const staged = status.staged ?? [];
  const unstaged = status.unstaged ?? [];
  const untracked = status.untracked ?? [];
  const conflicted = status.conflicted ?? [];
  const clean = staged.length + unstaged.length + untracked.length + conflicted.length === 0;
  const sync = syncText(status);
  const toggle = (path: string) => focusGitPath(expanded === path ? "" : path);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 flex-col gap-3.5 px-4 pt-4 pb-3">
        <GitFolderBar />
        <div className="flex items-center gap-2.5">
          <GitBranch className="size-4 shrink-0 text-text-secondary" aria-hidden />
          <span className="text-title-14 text-text-primary">{status.branch || "(detached)"}</span>
          {sync ? <span className="text-code-11 text-text-tertiary">{sync}</span> : null}
          {guard ? (
            <button
              type="button"
              aria-label={`Stop asking before commits on ${guard.pattern}`}
              title={`Committing on ${guard.pattern} asks first (${guard.env}). Click to stop asking.`}
              onClick={() => change(deleteGuard(guard.id))}
              className="inline-flex items-center gap-1.5 rounded-md bg-action-primary/12 px-2 py-0.75 text-caption-11 text-text-secondary hover:bg-action-primary/20"
            >
              <ShieldCheck className="size-(--icon-12) shrink-0" aria-hidden />
              {guard.pattern} · asks before committing
            </button>
          ) : status.branch ? (
            <Button
              variant="ghost"
              title="Ask before every commit on this branch"
              // prod is the tag this is nearly always for; an env tag that
              // does not require confirmation makes the guard a no-op, which
              // the backend already treats as "not gated".
              onClick={() => change(createGuard(status.branch, "prod"))}
            >
              Protect branch
            </Button>
          ) : null}
          <span className="min-w-0 flex-1" />
          <Button
            variant="outline"
            disabled={busy !== "" || !remote}
            title={
              remote
                ? "Goes over the network and changes nothing in the work tree"
                : "This repository has no remote"
            }
            onClick={() => network("fetch", gitFetch(remote), `Fetched ${remote}`)}
          >
            Fetch
          </Button>
          <Button
            variant="outline"
            disabled={busy !== "" || !remote}
            title="Fast-forward only"
            onClick={() => network("pull", gitPull(remote), `Pulled ${remote}`)}
          >
            Pull
          </Button>
          <Button
            disabled={busy !== "" || !remote || !status.branch}
            onClick={() =>
              network(
                "push",
                // Without an upstream the push has to set one, or the branch
                // is pushed and still reports as untracked.
                gitPush(remote, status.branch, !status.upstream),
                `Pushed ${status.branch} to ${remote}`,
              )
            }
          >
            Push
          </Button>
        </div>
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto px-4 pb-3">
        {clean ? <EmptyState icon={GitBranch} title="Nothing changed" body="The work tree is clean." /> : null}

        {conflicted.length > 0 ? (
          <ChangeSection
            title="Conflicts · resolve before committing"
            count={conflicted.length}
            tone="warning"
            actions={
              <HeadingAction
                title="Throw the whole merge away and go back to before it started"
                onClick={() => change(gitAbortMerge())}
              >
                Abort merge
              </HeadingAction>
            }
          >
            {conflicted.map((path) => (
              <ChangeRow
                key={path}
                path={path}
                kind="conflict"
                action="Resolve"
                onAction={() => setResolving(path)}
              />
            ))}
          </ChangeSection>
        ) : null}

        {staged.length > 0 ? (
          <ChangeSection
            title="Staged"
            count={staged.length}
            actions={<HeadingAction onClick={() => change(gitUnstage(staged))}>Unstage all</HeadingAction>}
          >
            {staged.map((path) => (
              <ChangeRow
                key={path}
                path={path}
                kind={markKind("staged", status.files[path])}
                action="Unstage"
                onAction={() => change(gitUnstage([path]))}
              />
            ))}
          </ChangeSection>
        ) : null}

        {unstaged.length > 0 ? (
          <ChangeSection
            title="Changes"
            count={unstaged.length}
            actions={<HeadingAction onClick={() => change(gitStage(unstaged))}>Stage all</HeadingAction>}
          >
            {unstaged.map((path) => (
              <ChangeRow
                key={path}
                path={path}
                kind={markKind("unstaged", status.files[path])}
                action="Stage"
                onAction={() => change(gitStage([path]))}
                open={expanded === path}
                onToggle={() => toggle(path)}
              >
                <HunkList path={path} onStaged={bumpGitRevision} onError={(m) => setError(m)} />
              </ChangeRow>
            ))}
          </ChangeSection>
        ) : null}

        {untracked.length > 0 ? (
          <ChangeSection
            title="Untracked"
            count={untracked.length}
            actions={<HeadingAction onClick={() => change(gitStage(untracked))}>Stage all</HeadingAction>}
          >
            {untracked.map((path) => (
              <ChangeRow
                key={path}
                path={path}
                kind="untracked"
                action="Stage"
                onAction={() => change(gitStage([path]))}
              />
            ))}
          </ChangeSection>
        ) : null}
      </div>

      <footer className="flex shrink-0 flex-col gap-2 px-4 pt-2 pb-4">
        {error ? (
          <p className="text-caption-11 text-status-danger" role="alert">
            {error}
          </p>
        ) : notice ? (
          <p className="text-caption-11 text-text-tertiary" role="status">
            {notice}
          </p>
        ) : null}
        <Textarea
          value={message}
          onChange={(event) => setMessage(event.target.value)}
          placeholder="Commit message"
          rows={3}
          aria-label="Commit message"
          className="min-h-17 rounded-lg border-0 bg-state-soft px-3 py-2.5 text-body-13 dark:bg-state-soft"
        />
        <div className="flex items-center gap-3">
          <button
            type="button"
            disabled={staged.length === 0}
            // The seeded body is the list of paths — a statement of fact. The
            // subject line stays the person's to write.
            onClick={() => setMessage((current) => current + seedMessage(staged))}
            className="text-caption-11 text-text-secondary transition-colors duration-(--motion-fast) ease-(--ease-out-strong) hover:text-text-primary disabled:pointer-events-none disabled:opacity-38"
          >
            Seed from staged
          </button>
          <span className="min-w-0 flex-1" />
          <span className="text-caption-11 text-text-tertiary">
            {staged.length === 0
              ? "Nothing staged"
              : `${staged.length} staged · will commit on ${status.branch || "a detached HEAD"}`}
          </span>
          <Button
            disabled={staged.length === 0 || message.trim() === "" || conflicted.length > 0}
            onClick={() =>
              change(
                gitCommit(message).then(() => {
                  setMessage("");
                }),
              )
            }
          >
            Commit
            <ArrowRight aria-hidden />
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
