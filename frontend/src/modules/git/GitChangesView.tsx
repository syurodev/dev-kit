import { useCallback, useEffect, useState, type ReactNode } from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
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
  gitPull,
  gitPush,
  gitStage,
  gitStageHunk,
  gitUnstage,
  seedMessage,
} from "@/bridge/git";
import { cn } from "@/lib/utils";
import { useModuleFolder } from "@/stores/workFolder";
import { buildPatch, parsePatch, type FilePatch, type Hunk } from "./hunks";
import { GitBranchMenu } from "./GitBranchMenu";
import { GitConflictPanel } from "./GitConflictPanel";
import { GitFolderBar } from "./GitFolderGate";
import { HeadingAction, RowAction, StatusMark } from "./GitParts";
import { focusGitPath, useGitFocus } from "./gitFocus";
import { bumpGitRevision, useGitRevision } from "./gitRevision";
import { markKind, splitPath, syncText, type MarkKind } from "./rail";
import { refreshGitRepo, useGitRepo } from "./gitRepo";
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

/** A list this long is cut, and the rest is one click away: a work tree with a build folder untracked can hold thousands. */
const SECTION_ROWS = 200;

/** One list of files under a heading, in a card — Staged, Changes, Untracked. */
function ChangeSection({
  title,
  count,
  tone = "default",
  actions,
  limit,
  onMore,
  children,
}: {
  title: string;
  count: number;
  tone?: "default" | "warning";
  actions?: ReactNode;
  /** How many of `count` rows `children` holds; fewer means there is more to show. */
  limit?: number;
  onMore?: () => void;
  children: ReactNode;
}) {
  // Rows that come and go — a file staged, a hunk done — slide instead of
  // cutting; the same 180 ms as every other list in the app.
  const [list] = useAutoAnimate<HTMLUListElement>({ duration: 180, easing: "cubic-bezier(0.23, 1, 0.32, 1)" });
  const hidden = limit !== undefined && count > limit ? count - limit : 0;
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
          <span className="text-code-11 tabular-nums opacity-70">{count}</span>
        </h3>
        {actions ? <div className="flex items-center gap-3">{actions}</div> : null}
      </header>
      <ul ref={list} className="flex flex-col rounded-lg border border-border-subtle bg-surface-well p-1">
        {children}
        {hidden > 0 ? (
          <li>
            <button
              type="button"
              onClick={onMore}
              className="h-9 w-full rounded-md text-caption-11 text-text-tertiary transition-colors duration-(--motion-fast) ease-(--ease-out-strong) hover:bg-state-soft hover:text-text-primary"
            >
              <span className="tabular-nums">{hidden}</span> more — show {Math.min(hidden, SECTION_ROWS)}
            </button>
          </li>
        ) : null}
      </ul>
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
      <div className="flex h-9 items-center gap-2.5 pr-3 pl-3">
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
 * What is shown is what Git last read (gitRepo), shared with the rail: every
 * change made here — and by the rail, which stages too — goes through
 * bumpGitRevision(), and this view asks for a read when the revision moves. The
 * commit message is not state of this component: typing in it must not redraw
 * the file lists (see Composer).
 */
export function GitChangesView() {
  const revision = useGitRevision((state) => state.revision);
  const expanded = useGitFocus((state) => state.path);
  const { folder } = useModuleFolder("git");
  const entry = useGitRepo(folder);
  const snapshot = entry?.snapshot ?? null;
  const [unavailable, setUnavailable] = useState("");
  // The unmerged path being resolved. The panel takes the whole view while it
  // is open: a three-pane merge does not fit beside a file list, and nothing
  // else here can be done until the merge is finished or thrown away.
  const [resolving, setResolving] = useState("");
  // The network action in flight, "" when none. One at a time: a pull under a
  // push is a race over the same branch.
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  // What an action here failed with. A failed read is the entry's own error.
  const [actionError, setActionError] = useState("");
  // How many rows each section shows, when it has been asked for more.
  const [limits, setLimits] = useState<Record<string, number>>({});

  // Asked alongside the first read, not before it: a missing git makes the read
  // fail as well, and this is what says why.
  useEffect(() => {
    gitAvailability()
      .then((availability) => {
        if (!availability.ok) {
          setUnavailable(availability.reason || "No usable git was found on this machine.");
        }
      })
      .catch((e: Error) => setUnavailable(e.message));
  }, []);

  useEffect(() => {
    void refreshGitRepo(folder, String(revision));
  }, [folder, revision]);

  const fail = (e: Error) => setActionError(e.message);

  /** Runs a change to the repository, then has everything that shows it read again. */
  function change(work: Promise<unknown>) {
    setActionError("");
    work.then(bumpGitRevision).catch(fail);
  }

  function network(key: string, work: Promise<void>, done: string) {
    setBusy(key);
    setNotice("");
    setActionError("");
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
  if (!snapshot && entry?.error) {
    return <EmptyState icon={GitBranch} title="Nothing to show" body={entry.error} />;
  }
  if (!snapshot) {
    // Nothing read yet and nothing remembered: hold the bar's place so the
    // view does not open on an empty pane and then shift down.
    return (
      <div className="flex h-full min-h-0 flex-col px-4 pt-4">
        <GitFolderBar />
      </div>
    );
  }

  if (resolving) {
    return (
      <GitConflictPanel
        path={resolving}
        branch={snapshot.status.branch}
        onClose={() => setResolving("")}
        onResolved={() => {
          setResolving("");
          bumpGitRevision();
        }}
      />
    );
  }

  const { status, guards, remotes } = snapshot;
  // The guard covering the branch you are on, if any. Its absence is the
  // default and is not a problem to report — committing simply does not ask.
  const guard = guards.find((candidate) => matchesBranch(candidate.pattern, status.branch));
  const remote = remoteFor(
    status.upstream,
    remotes.map((r) => r.name),
  );

  const staged = status.staged ?? [];
  const unstaged = status.unstaged ?? [];
  const untracked = status.untracked ?? [];
  const conflicted = status.conflicted ?? [];
  const clean = staged.length + unstaged.length + untracked.length + conflicted.length === 0;
  const sync = syncText(status);
  const toggle = (path: string) => focusGitPath(expanded === path ? "" : path);
  const limitOf = (key: string) => limits[key] ?? SECTION_ROWS;
  const more = (key: string) => () => setLimits((current) => ({ ...current, [key]: limitOf(key) + SECTION_ROWS }));
  const problem = actionError || entry?.error || "";

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 flex-col gap-3.5 px-4 pt-4 pb-3">
        <GitFolderBar />
        <div className="flex items-center gap-2.5">
          <GitBranchMenu
            branch={status.branch}
            dirty={staged.length + unstaged.length + conflicted.length > 0}
            onChanged={bumpGitRevision}
            onError={setActionError}
          />
          {sync ? <span className="text-code-11 tabular-nums text-text-tertiary">{sync}</span> : null}
          {guard ? (
            <button
              type="button"
              aria-label={`Stop asking before commits on ${guard.pattern}`}
              title={`Committing on ${guard.pattern} asks first (${guard.env}). Click to stop asking.`}
              onClick={() => change(deleteGuard(guard.id))}
              className="inline-flex items-center gap-1.5 rounded-md bg-action-primary/12 px-2 py-0.75 text-caption-11 text-text-secondary transition-colors duration-(--motion-fast) ease-(--ease-out-strong) hover:bg-action-primary/20"
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

      <div className="scrollbar-gutter-stable flex min-h-0 flex-1 flex-col gap-4 overflow-auto px-4 pb-3">
        {clean ? <EmptyState icon={GitBranch} title="Nothing changed" body="The work tree is clean." /> : null}

        {conflicted.length > 0 ? (
          <ChangeSection
            title="Conflicts · resolve before committing"
            count={conflicted.length}
            tone="warning"
            limit={limitOf("conflicted")}
            onMore={more("conflicted")}
            actions={
              <HeadingAction
                title="Throw the whole merge away and go back to before it started"
                onClick={() => change(gitAbortMerge())}
              >
                Abort merge
              </HeadingAction>
            }
          >
            {conflicted.slice(0, limitOf("conflicted")).map((path) => (
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
            limit={limitOf("staged")}
            onMore={more("staged")}
            actions={<HeadingAction onClick={() => change(gitUnstage(staged))}>Unstage all</HeadingAction>}
          >
            {staged.slice(0, limitOf("staged")).map((path) => (
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
            limit={limitOf("unstaged")}
            onMore={more("unstaged")}
            actions={<HeadingAction onClick={() => change(gitStage(unstaged))}>Stage all</HeadingAction>}
          >
            {unstaged.slice(0, limitOf("unstaged")).map((path) => (
              <ChangeRow
                key={path}
                path={path}
                kind={markKind("unstaged", status.files[path])}
                action="Stage"
                onAction={() => change(gitStage([path]))}
                open={expanded === path}
                onToggle={() => toggle(path)}
              >
                <HunkList path={path} onStaged={bumpGitRevision} onError={setActionError} />
              </ChangeRow>
            ))}
          </ChangeSection>
        ) : null}

        {untracked.length > 0 ? (
          <ChangeSection
            title="Untracked"
            count={untracked.length}
            limit={limitOf("untracked")}
            onMore={more("untracked")}
            actions={<HeadingAction onClick={() => change(gitStage(untracked))}>Stage all</HeadingAction>}
          >
            {untracked.slice(0, limitOf("untracked")).map((path) => (
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
        {/* One line is always here, so a message arriving does not push the
            composer down and shrink the list above it. */}
        <p
          className={cn("min-h-4 text-caption-11", problem ? "text-status-danger" : "text-text-tertiary")}
          role={problem ? "alert" : "status"}
        >
          {problem || notice}
        </p>
        <Composer
          staged={staged}
          branch={status.branch}
          blocked={conflicted.length > 0}
          onCommit={(message) => {
            setActionError("");
            return gitCommit(message)
              .then(() => bumpGitRevision())
              .catch((e: Error) => {
                fail(e);
                // The message is kept: a refused commit is not a lost one.
                throw e;
              });
          }}
        />
      </footer>
    </div>
  );
}

/**
 * The commit message and the button that commits it. The message is state of
 * this component and no other: it changes with every keystroke, and if the view
 * held it the lists of files above would be redrawn for each one.
 */
function Composer({
  staged,
  branch,
  blocked,
  onCommit,
}: {
  staged: string[];
  branch: string;
  /** Unmerged files: the backend refuses a commit while any remain. */
  blocked: boolean;
  /** Rejects when the commit fails, so the message is kept. */
  onCommit: (message: string) => Promise<void>;
}) {
  const [message, setMessage] = useState("");
  return (
    <>
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
          {staged.length === 0 ? (
            "Nothing staged"
          ) : (
            <>
              <span className="tabular-nums">{staged.length}</span> staged · will commit on{" "}
              {branch || "a detached HEAD"}
            </>
          )}
        </span>
        <Button
          disabled={staged.length === 0 || message.trim() === "" || blocked}
          onClick={() => {
            onCommit(message).then(() => setMessage(""), () => undefined);
          }}
        >
          Commit
          <ArrowRight aria-hidden />
        </Button>
      </div>
    </>
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
