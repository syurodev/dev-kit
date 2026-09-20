import { useEffect, useMemo, useState } from "react";
import { FileDiff, GitCommitHorizontal, Hash } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DiffSplitPanes } from "@/components/ui/diff";
import { EmptyState } from "@/components/ui/empty-state";
import { changedRuns, splitPairs } from "@/lib/diffHunks";
import { cn } from "@/lib/utils";
import { gitCommitInfo, gitDiff, gitRevPatch, type Change, type Commit } from "@/bridge/git";
import { StatePill, StatusMark } from "./GitParts";
import { changeSummary, commitBody, patchLines } from "./commitView";
import { parsePatch, type FilePatch } from "./hunks";
import { changeMarkKind, splitPath } from "./rail";

const KIND_MARK: Record<Change["kind"], string> = {
  added: "A",
  modified: "M",
  deleted: "D",
  typechange: "T",
};

/**
 * A commit, or with no revision the work tree: per-file change counts. A binary
 * file reports itself rather than showing 0/0.
 */
export function GitDiffView({ params }: { params?: Record<string, unknown> }) {
  const rev = typeof params?.rev === "string" ? params.rev : "";
  return rev ? <CommitView rev={rev} /> : <WorkTreeDiff />;
}

function WorkTreeDiff() {
  const [changes, setChanges] = useState<Change[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    gitDiff("", false)
      .then(setChanges)
      .catch((e: Error) => setError(e.message));
  }, []);

  if (error) {
    return <EmptyState icon={FileDiff} title="No diff" body={error} />;
  }
  if (!changes) {
    return null;
  }
  if (changes.length === 0) {
    return <EmptyState icon={FileDiff} title="Nothing changed" body="There is no diff to show." />;
  }
  return (
    <ul className="h-full overflow-auto">
      {changes.map((change) => (
        <li
          key={change.path}
          className="flex items-center gap-3 border-b border-border-subtle px-3 py-1.5 last:border-b-0"
        >
          <span className="w-4 text-center text-xs text-fg-muted">{KIND_MARK[change.kind]}</span>
          <span className="min-w-0 flex-1 truncate font-mono text-xs">{change.path}</span>
          {change.added < 0 ? (
            <span className="text-xs text-fg-muted">binary</span>
          ) : (
            <span className="text-xs">
              <span className="text-emerald-500">+{change.added}</span>{" "}
              <span className="text-red-500">-{change.removed}</span>
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}

/**
 * Figma `Git / Commit Content`: who made a commit and why, the files it
 * touched, and the one you pick as it was and as it became.
 *
 * The two panes are the shared two-pane diff — the same layout Notes history and
 * Sync's resolve use — drawn from the commit's own patch for the file, so a
 * hunk that changed a file in three places shows three, with the line numbers
 * they have in the file.
 */
function CommitView({ rev }: { rev: string }) {
  const [commit, setCommit] = useState<Commit | null>(null);
  const [changes, setChanges] = useState<Change[] | null>(null);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState("");

  useEffect(() => {
    let live = true;
    setCommit(null);
    setChanges(null);
    setSelected("");
    Promise.all([gitCommitInfo(rev), gitDiff(rev, false)])
      .then(([info, files]) => {
        if (!live) return;
        setCommit(info);
        setChanges(files);
        setSelected(files[0]?.path ?? "");
      })
      .catch((e: Error) => live && setError(e.message));
    return () => {
      live = false;
    };
  }, [rev]);

  if (error) {
    return <EmptyState icon={FileDiff} title="No commit" body={error} />;
  }
  if (!commit || !changes) {
    return null;
  }

  const { text, task } = commitBody(commit.body);
  return (
    <div className="flex h-full min-h-0 flex-col gap-3.5 px-5 py-4.5">
      <div className="flex items-center gap-2.5">
        <GitCommitHorizontal className="size-4.5 shrink-0 text-text-secondary" aria-hidden />
        <h2 className="min-w-0 flex-1 truncate text-title-16 text-text-primary" title={commit.subject}>
          {commit.subject}
        </h2>
        <CopyShaButton sha={commit.sha} />
      </div>

      <div className="flex items-center gap-2">
        <StatePill className="font-mono" icon={<Hash className="size-(--icon-12) shrink-0" aria-hidden />}>
          {commit.short}
        </StatePill>
        <span className="text-label-12 text-text-secondary">{commit.author}</span>
        <span className="text-code-11 text-text-tertiary">{commit.date.slice(0, 16).replace("T", " ")}</span>
        {commit.parent ? <span className="text-code-11 text-text-tertiary">parent {commit.parent}</span> : null}
        <span className="min-w-0 flex-1" />
        {task ? (
          <StatePill className="font-mono" icon={<Hash className="size-(--icon-12) shrink-0" aria-hidden />}>
            Task: {task}
          </StatePill>
        ) : null}
      </div>

      {text ? <p className="text-body-13 whitespace-pre-line text-text-secondary">{text}</p> : null}
      <p className="text-caption-11 text-text-tertiary">{changeSummary(changes)}</p>

      {changes.length === 0 ? (
        <EmptyState icon={FileDiff} title="No file changes" body="This commit changes no files, or only merges." />
      ) : (
        <div className="flex min-h-0 flex-1 gap-3.5">
          <ul className="flex w-75 shrink-0 flex-col overflow-auto rounded-lg border border-border-subtle bg-surface-well">
            {changes.map((change) => (
              <FileRow
                key={change.path}
                change={change}
                selected={change.path === selected}
                onSelect={() => setSelected(change.path)}
              />
            ))}
          </ul>
          <div className="min-w-0 flex-1 overflow-auto">
            <FileDiffPanes rev={rev} path={selected} after={commit.short} before={commit.parent} />
          </div>
        </div>
      )}
    </div>
  );
}

function CopyShaButton({ sha }: { sha: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1500);
    return () => window.clearTimeout(timer);
  }, [copied]);
  return (
    <Button
      variant="outline"
      onClick={() => {
        // A refusal (no permission, no secure context) is not worth a message:
        // the hash is on screen and can be selected.
        navigator.clipboard
          .writeText(sha)
          .then(() => setCopied(true))
          .catch(() => undefined);
      }}
    >
      {copied ? "Copied" : "Copy SHA"}
    </Button>
  );
}

function FileRow({ change, selected, onSelect }: { change: Change; selected: boolean; onSelect: () => void }) {
  const { name, dir } = splitPath(change.path);
  return (
    <li>
      <button
        type="button"
        onClick={onSelect}
        aria-current={selected ? "true" : undefined}
        title={change.path}
        className={cn(
          "flex w-full items-center gap-2 px-2.5 py-1.75 text-left",
          selected ? "bg-state-soft" : "hover:bg-state-soft",
        )}
      >
        <StatusMark kind={changeMarkKind(change.kind)} />
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-code-12 text-text-primary">{name}</span>
          {dir ? <span className="truncate text-code-11 text-text-tertiary">{dir}</span> : null}
        </span>
        {change.added < 0 ? (
          <span className="shrink-0 text-code-11 text-text-tertiary">binary</span>
        ) : (
          <>
            {change.added > 0 ? <span className="shrink-0 text-code-11 text-status-success">+{change.added}</span> : null}
            {change.removed > 0 ? (
              <span className="shrink-0 text-code-11 text-status-danger">−{change.removed}</span>
            ) : null}
          </>
        )}
      </button>
    </li>
  );
}

/** One file of the commit, before and after. Reads its patch when the file changes. */
function FileDiffPanes({ rev, path, before, after }: { rev: string; path: string; before: string; after: string }) {
  const [patch, setPatch] = useState<{ path: string; value: FilePatch } | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!path) return;
    let live = true;
    setError("");
    gitRevPatch(rev, path)
      .then((text) => live && setPatch({ path, value: parsePatch(text) }))
      .catch((e: Error) => live && setError(e.message));
    return () => {
      live = false;
    };
  }, [rev, path]);

  const current = patch?.path === path ? patch.value : null;
  const lines = useMemo(() => (current ? patchLines(current) : []), [current]);
  const pairs = useMemo(() => splitPairs(lines), [lines]);
  const runs = useMemo(() => changedRuns(pairs), [pairs]);

  if (error) {
    return <p className="text-caption-11 text-status-danger">{error}</p>;
  }
  if (!current) {
    return null;
  }
  if (current.binary || current.hunks.length === 0) {
    return (
      <EmptyState
        icon={FileDiff}
        title={current.binary ? "Binary file" : "No lines to show"}
        body={current.binary ? "There is no text to compare." : "Only the file's mode or name changed."}
      />
    );
  }
  return (
    <DiffSplitPanes
      pairs={pairs}
      runs={runs}
      leftLabel={`BEFORE · ${before || "none"}`}
      rightLabel={`AFTER · ${after}`}
    />
  );
}
