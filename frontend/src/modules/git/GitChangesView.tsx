import { FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { gitUI, useGitBusy, useGitDraft, useGitOperation } from "./gitUI";
import { openFileDiff, openHistory } from "./documents";
import { useEffect, useMemo, useState, useId, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { ArrowRight, ChevronDown, ChevronRight, GitBranch, ShieldCheck, Ellipsis } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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
  gitStash,
  gitStashPop,
  seedMessage,
} from "@/bridge/git";
import { cn } from "@/lib/utils";
import { useModuleFolder } from "@/stores/workFolder";
import { patchLines } from "./commitView";
import { usePatchHighlight } from "./gitSyntax";
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
  const { folder } = useModuleFolder("git");
  const busy = useGitBusy();
  const revision = useGitRevision((state) => state.revision);
  const query = useQuery({
    queryKey: ["git", "patch", folder, "", path, false, revision],
    queryFn: () => gitFilePatch(path),
  });
  const patch = useMemo(() => (query.data === undefined ? null : parsePatch(query.data)), [query.data]);

  if (query.error)
    return (
      <div className="px-3 pb-2">
        <FeedbackBanner
          tone="error"
          title="Could not read hunks"
          action={
            <Button variant="outline" onClick={() => void query.refetch()}>
              Retry
            </Button>
          }
        >
          {String(query.error instanceof Error ? query.error.message : query.error)}
        </FeedbackBanner>
      </div>
    );
  if (!patch)
    return (
      <div className="px-3 pb-2 pl-10">
        <PendingStatus>Reading the diff…</PendingStatus>
      </div>
    );
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
    void gitUI
      .run(folder, "Staging hunk…", async () => {
        await gitStageHunk(text);
        onStaged();
      })
      .then((ok) => {
        if (!ok) onError(gitUI.useStore.getState().operations[folder]?.error ?? "");
      });
  }

  return (
    <div className="flex flex-col gap-2 px-3 pb-3">
      {patch.hunks.map((hunk, index) => (
        <HunkCard
          key={`${index}:${hunk.header}`}
          path={path}
          hunk={hunk}
          busy={busy || query.isFetching}
          onStage={() => stage(index)}
        />
      ))}
    </div>
  );
}

/** One hunk, standing in for the single-hunk patch usePatchHighlight expects. */
function soloPatch(hunk: Hunk): FilePatch {
  return { head: [], hunks: [hunk], binary: false };
}

function HunkCard({ path, hunk, busy, onStage }: { path: string; hunk: Hunk; busy: boolean; onStage: () => void }) {
  // Memoized on hunk, not recreated per render: usePatchHighlight's effect
  // keys off this object's identity, and a fresh one every render would never
  // let the effect settle — each highlight landing would trigger another
  // object, another effect run, another highlight, forever.
  const patch = useMemo(() => soloPatch(hunk), [hunk]);
  const highlight = usePatchHighlight(patch, path);
  const rows = useMemo(() => patchLines(patch), [patch]);
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
        {rows.slice(0, PREVIEW_LINES).map((row, index) => (
          <DiffLineRow
            key={index}
            row={row}
            segments={row.kind === "removed" ? highlight?.before.get(row.before) : highlight?.after.get(row.after)}
          />
        ))}
        {rows.length > PREVIEW_LINES ? (
          <div className="px-3 py-1 text-code-11 text-text-tertiary">…{rows.length - PREVIEW_LINES} more lines</div>
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
  const [list] = useAutoAnimate<HTMLUListElement>({
    duration: 180,
    easing: "cubic-bezier(0.23, 1, 0.32, 1)",
  });
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
  onReview,
  children,
}: {
  path: string;
  kind: MarkKind;
  action: string;
  onAction: () => void;
  /** Undefined for a row that cannot unfold. */
  open?: boolean;
  onToggle?: () => void;
  onReview?: () => void;
  children?: ReactNode;
}) {
  const busy = useGitBusy();
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
        {onReview && (
          <Button size="sm" variant="ghost" onClick={onReview} aria-label={`Review ${path}`}>
            Review
          </Button>
        )}
        <RowAction disabled={busy} onClick={onAction}>
          {action}
        </RowAction>
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
  const operation = useGitOperation(folder);
  const busy = useGitBusy();
  const visible = useModuleVisibility();
  const [moreOpen, setMoreOpen] = useState(false);
  useEffect(() => {
    if (!visible) setMoreOpen(false);
  }, [visible]);
  const [confirm, setConfirm] = useState<"stash" | "restore" | "abort" | null>(null);
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

  /** Runs a change to the repository, then has everything that shows it read again. */
  function change(work: () => Promise<unknown>, label = "Updating changes", notice = "") {
    setActionError("");
    return gitUI.run(
      folder,
      label,
      async () => {
        await work();
        bumpGitRevision();
      },
      notice,
    );
  }
  function ask(action: "stash" | "restore" | "abort") {
    gitUI.clearFeedback(folder);
    setActionError("");
    setConfirm(action);
  }
  function network(key: string, work: () => Promise<void>, done: string) {
    void change(work, `${key[0].toUpperCase()}${key.slice(1)} in progress…`, done);
  }

  if (unavailable) {
    return <EmptyState icon={GitBranch} title="Git is not available" body={unavailable} />;
  }
  if (!snapshot && entry?.error) {
    return (
      <EmptyState
        icon={GitBranch}
        title="Could not read repository"
        body={entry.error}
        action={{
          label: "Retry",
          onClick: () => void refreshGitRepo(folder, `retry:${Date.now()}`),
        }}
      />
    );
  }
  if (!snapshot) {
    // Nothing read yet and nothing remembered: hold the bar's place so the
    // view does not open on an empty pane and then shift down.
    return (
      <div className="flex h-full min-h-0 flex-col px-4 pt-4">
        <GitFolderBar />
        <div className="py-3">
          <PendingStatus>Reading repository…</PendingStatus>
        </div>
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
  const more = (key: string) => () =>
    setLimits((current) => ({
      ...current,
      [key]: limitOf(key) + SECTION_ROWS,
    }));
  const problem = operation.error || actionError || entry?.error || "";

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 flex-col gap-3.5 px-4 pt-4 pb-3">
        <GitFolderBar />
        <div className="flex flex-wrap items-center gap-2.5">
          <GitBranchMenu
            disabled={busy}
            branch={status.branch}
            dirty={staged.length + unstaged.length + conflicted.length > 0}
            onChanged={bumpGitRevision}
            onError={setActionError}
          />
          {sync ? <span className="text-code-11 tabular-nums text-text-tertiary">{sync}</span> : null}
          {guard && (
            <span
              className="inline-flex items-center gap-1.5 text-caption-11 text-text-secondary"
              title={`Asks before commits (${guard.env})`}
            >
              <ShieldCheck className="size-3" />
              Protected
            </span>
          )}
          <span className="min-w-0 flex-1" />
          <div className="flex flex-wrap items-center gap-1.5">
            <Button variant="ghost" onClick={openHistory}>
              History
            </Button>
            <DropdownMenu open={moreOpen && visible} onOpenChange={setMoreOpen}>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon-sm" aria-label="More Git actions" disabled={busy}>
                  <Ellipsis />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem disabled={clean || conflicted.length > 0} onSelect={() => ask("stash")}>
                  Stash changes…
                </DropdownMenuItem>
                <DropdownMenuItem disabled={conflicted.length > 0} onSelect={() => ask("restore")}>
                  Restore latest stash…
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  disabled={!status.branch}
                  icon={<ShieldCheck />}
                  onSelect={() =>
                    void change(() => (guard ? deleteGuard(guard.id) : createGuard(status.branch, "prod")))
                  }
                >
                  {guard ? "Remove branch protection" : "Protect branch"}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button
              variant="outline"
              disabled={busy || !remote}
              title={
                remote ? "Goes over the network and changes nothing in the work tree" : "This repository has no remote"
              }
              onClick={() => network("fetch", () => gitFetch(remote), `Fetched ${remote}`)}
            >
              Fetch
            </Button>
            <Button
              variant="outline"
              disabled={busy || !remote}
              title="Fast-forward only"
              onClick={() => network("pull", () => gitPull(remote), `Pulled ${remote}`)}
            >
              Pull
            </Button>
            <Button
              disabled={busy || !remote || !status.branch}
              onClick={() =>
                network(
                  "push",
                  // Without an upstream the push has to set one, or the branch
                  // is pushed and still reports as untracked.
                  () => gitPush(remote, status.branch, !status.upstream),
                  `Pushed ${status.branch} to ${remote}`,
                )
              }
            >
              Push
            </Button>
          </div>
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
                disabled={busy}
                title="Throw the whole merge away and go back to before it started"
                onClick={() => ask("abort")}
              >
                Abort merge
              </HeadingAction>
            }
          >
            {conflicted.slice(0, limitOf("conflicted")).map((path) => (
              <ChangeRow key={path} path={path} kind="conflict" action="Resolve" onAction={() => setResolving(path)} />
            ))}
          </ChangeSection>
        ) : null}

        {staged.length > 0 ? (
          <ChangeSection
            title="Staged"
            count={staged.length}
            limit={limitOf("staged")}
            onMore={more("staged")}
            actions={
              <HeadingAction disabled={busy} onClick={() => change(() => gitUnstage(staged))}>
                Unstage all
              </HeadingAction>
            }
          >
            {staged.slice(0, limitOf("staged")).map((path) => (
              <ChangeRow
                key={path}
                path={path}
                onReview={() => openFileDiff(path, true)}
                kind={markKind("staged", status.files[path])}
                action="Unstage"
                onAction={() => change(() => gitUnstage([path]))}
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
            actions={
              <HeadingAction disabled={busy} onClick={() => change(() => gitStage(unstaged))}>
                Stage all
              </HeadingAction>
            }
          >
            {unstaged.slice(0, limitOf("unstaged")).map((path) => (
              <ChangeRow
                key={path}
                path={path}
                onReview={() => openFileDiff(path, false)}
                kind={markKind("unstaged", status.files[path])}
                action="Stage"
                onAction={() => change(() => gitStage([path]))}
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
            actions={
              <HeadingAction disabled={busy} onClick={() => change(() => gitStage(untracked))}>
                Stage all
              </HeadingAction>
            }
          >
            {untracked.slice(0, limitOf("untracked")).map((path) => (
              <ChangeRow
                key={path}
                path={path}
                kind="untracked"
                action="Stage"
                onAction={() => change(() => gitStage([path]))}
              />
            ))}
          </ChangeSection>
        ) : null}
      </div>

      <footer className="flex shrink-0 flex-col gap-2 px-4 pt-2 pb-4">
        {busy && <PendingStatus>{operation.label || "Updating repository…"}</PendingStatus>}
        {problem && !confirm && (
          <FeedbackBanner tone="error" title="Git action failed">
            {problem}
          </FeedbackBanner>
        )}
        {operation.notice && <FeedbackBanner tone="success" title={operation.notice} />}
        <Composer
          folder={folder}
          busy={busy}
          staged={staged}
          branch={status.branch}
          blocked={conflicted.length > 0}
          onCommit={(message) =>
            change(
              async () => {
                await gitCommit(message);
                gitUI.setDraft(folder, { message: "" });
              },
              "Committing…",
              "Commit created",
            )
          }
        />
      </footer>
      <ConfirmDialog
        open={!!confirm && visible}
        onClose={() => setConfirm(null)}
        title={
          confirm === "stash"
            ? "Stash current changes?"
            : confirm === "restore"
              ? "Restore the latest stash?"
              : "Abort this merge?"
        }
        confirmLabel={confirm === "stash" ? "Stash changes" : confirm === "restore" ? "Restore stash" : "Abort merge"}
        pendingLabel="Updating repository…"
        pending={busy}
        error={operation.error}
        onConfirm={() => {
          const action =
            confirm === "stash"
              ? () => gitStash("DevKit: work in progress")
              : confirm === "restore"
                ? gitStashPop
                : gitAbortMerge;
          void change(
            action,
            "Updating repository…",
            confirm === "stash" ? "Changes stashed" : confirm === "restore" ? "Stash restored" : "Merge aborted",
          ).then((ok) => {
            if (ok) setConfirm(null);
            else bumpGitRevision();
          });
        }}
      >
        {confirm === "stash"
          ? "Saves staged, unstaged and untracked files, then clears the working tree. Restore stash brings the saved work back."
          : confirm === "restore"
            ? "Applies and removes the latest stash after a successful restore. Conflicts may require resolution; a failed restore keeps the stash."
            : "Returns the repository to its state before this merge. Changes made during the merge can be lost."}
      </ConfirmDialog>
    </div>
  );
}

/**
 * The commit message and the button that commits it. The message is state of
 * a session draft scoped to the repository, so switching documents keeps it.
 */
function Composer({
  folder,
  busy,
  staged,
  branch,
  blocked,
  onCommit,
}: {
  folder: string;
  busy: boolean;
  staged: string[];
  branch: string;
  /** Unmerged files: the backend refuses a commit while any remain. */
  blocked: boolean;
  /** Rejects when the commit fails, so the message is kept. */
  onCommit: (message: string) => Promise<boolean>;
}) {
  const messageId = useId();
  const { message } = useGitDraft(folder);
  const setMessage = (value: string) => gitUI.setDraft(folder, { message: value });
  return (
    <>
      <Field>
        <FieldLabel htmlFor={messageId}>Commit message</FieldLabel>
        <Textarea
          id={messageId}
          disabled={busy}
          value={message}
          onChange={(event) => setMessage(event.target.value)}
          placeholder="Commit message"
          rows={3}
          aria-label="Commit message"
          className="min-h-17 rounded-lg border-0 bg-state-soft px-3 py-2.5 text-body-13 dark:bg-state-soft"
        />
      </Field>
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          disabled={busy || staged.length === 0}
          // The seeded body is the list of paths — a statement of fact. The
          // subject line stays the person's to write.
          onClick={() => setMessage(message + seedMessage(staged))}
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
          disabled={busy || staged.length === 0 || message.trim() === "" || blocked}
          onClick={() => {
            void onCommit(message);
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
