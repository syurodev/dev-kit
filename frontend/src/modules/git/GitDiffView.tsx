import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  FileDiff,
  GitCommitHorizontal,
  ChevronLeft,
  ChevronRight,
  Files,
  Copy,
  Check,
  Columns2,
  MessageSquareText,
  Minus,
  Plus,
  RefreshCw,
  Rows2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { DiffLineRow, DiffPane, DiffSplitPanes } from "@/components/ui/diff";
import { EmptyState } from "@/components/ui/empty-state";
import { FeedbackBanner, InlineFeedback, PendingStatus } from "@/components/ui/feedback";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { changedRuns, splitPairs } from "@/lib/diffHunks";
import { cn } from "@/lib/utils";
import { errorMessage } from "@/bridge/errors";
import {
  gitCommitInfo,
  gitDiff,
  gitFilePatch,
  gitIndexPatch,
  gitRevPatch,
  gitStage,
  gitUnstage,
  type Change,
} from "@/bridge/git";
import { useModuleFolder } from "@/stores/workFolder";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { IconAction, StatusMark } from "./GitParts";
import { changeSummary, commitBody, patchLines } from "./commitView";
import { usePatchHighlight } from "./gitSyntax";
import { parsePatch } from "./hunks";
import { changeMarkKind, splitPath } from "./rail";
import { gitUI, useGitBusy, type DiffMode } from "./gitUI";
import { bumpGitRevision, useGitRevision } from "./gitRevision";
import { refreshGitRepo, useGitRepo } from "./gitRepo";

const FILE_ROWS = 200;
const DIFF_LINES = 1500;

/** A changed file in the list: what git diffs, or an untracked file it does not, shown whole as a new file. */
type DiffEntry = Change & { untracked?: true };

/** Diff mode is a preference, not a second set of document tabs. */
export function GitDiffView({ params }: { params?: Record<string, unknown> }) {
  const rev = typeof params?.rev === "string" ? params.rev : "";
  const staged = params?.staged === true;
  const initialPath = typeof params?.path === "string" ? params.path : "";
  const { folder } = useModuleFolder("git");
  const revision = useGitRevision((s) => s.revision);
  const visible = useModuleVisibility();
  const preferredMode = gitUI.useStore((s) => s.diffMode);
  const [selected, setSelected] = useState(initialPath);
  const [search, setSearch] = useState("");
  const [shownFiles, setShownFiles] = useState(FILE_ROWS);
  const [pickerOpen, setPickerOpen] = useState(false);
  const paneRef = useRef<HTMLDivElement>(null);
  const [wide, setWide] = useState(false);
  const mode = preferredMode ?? (wide ? "split" : "unified");
  useEffect(() => {
    const element = paneRef.current;
    if (!element) return;
    const observer = new ResizeObserver((entries) => setWide(entries[0].contentRect.width >= 760));
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (!visible) setPickerOpen(false);
  }, [visible]);
  const filesQuery = useQuery({
    queryKey: ["git", "diff", folder, rev, staged, rev ? 0 : revision],
    queryFn: () => gitDiff(rev, staged),
  });
  // `git diff` never lists an untracked file, and the work tree's changes are
  // not all of them without those: they are read from what Git last read, the
  // same list the rail shows, and open as new files.
  const workTree = !rev && !staged;
  const repoEntry = useGitRepo(folder);
  useEffect(() => {
    if (workTree) void refreshGitRepo(folder, String(revision));
  }, [workTree, folder, revision]);
  const untracked = workTree ? (repoEntry?.snapshot?.status.untracked ?? null) : null;
  const busy = useGitBusy();
  const [actionError, setActionError] = useState("");
  const commitQuery = useQuery({
    queryKey: ["git", "commit", folder, rev],
    queryFn: () => gitCommitInfo(rev),
    enabled: !!rev,
  });
  const tracked = filesQuery.data;
  const changes = useMemo<DiffEntry[]>(
    () => [
      ...(tracked ?? []),
      ...(untracked ?? []).map(
        (path): DiffEntry => ({ path, kind: "added", added: 0, removed: 0, mode: "", untracked: true }),
      ),
    ],
    [tracked, untracked],
  );
  const path = changes.some((file) => file.path === selected) ? selected : (changes[0]?.path ?? "");
  const index = changes.findIndex((file) => file.path === path);
  const files = changes.filter((file) => file.path.toLowerCase().includes(search.trim().toLowerCase()));
  const commit = commitQuery.data;
  const body = commit ? commitBody(commit.body) : null;
  const loading = filesQuery.isPending;
  const current = changes[index];
  // Staging or unstaging the file on screen: the list is read again by the
  // revision, and the file moves to the other scope, so the next one is shown.
  const canMove = !rev && !!current;
  function toggleStage() {
    if (!current) return;
    setActionError("");
    void gitUI
      .run(folder, staged ? "Unstaging…" : "Staging…", async () => {
        await (staged ? gitUnstage([current.path]) : gitStage([current.path]));
        bumpGitRevision();
      })
      .then((ok) => {
        if (!ok) setActionError(gitUI.useStore.getState().operations[folder]?.error ?? "");
      });
  }
  const error = filesQuery.error || (rev ? commitQuery.error : null);
  const choose = (value: string) => {
    setSelected(value);
    setPickerOpen(false);
  };
  function move(delta: number) {
    choose(changes[index + delta]?.path ?? path);
  }

  const fileList = (
    <div className="flex h-full min-h-0 flex-col gap-2 p-2">
      <Input
        inputSize="sm"
        aria-label="Filter changed files"
        placeholder="Filter files…"
        value={search}
        onChange={(e) => {
          setSearch(e.target.value);
          setShownFiles(FILE_ROWS);
        }}
      />
      <ScrollArea className="min-h-0 flex-1">
        <ul aria-label="Changed files">
          {files.slice(0, shownFiles).map((file) => (
            <FileRow key={file.path} change={file} selected={path === file.path} onSelect={() => choose(file.path)} />
          ))}
          {files.length > shownFiles && (
            <li>
              <Button variant="ghost" className="w-full" onClick={() => setShownFiles((n) => n + FILE_ROWS)}>
                Show {Math.min(FILE_ROWS, files.length - shownFiles)} more
              </Button>
            </li>
          )}
        </ul>
        {files.length === 0 && (
          <EmptyState
            size="rail"
            title="No matching files"
            action={{ label: "Clear filter", onClick: () => setSearch("") }}
          />
        )}
      </ScrollArea>
    </div>
  );
  return (
    <div ref={paneRef} className="flex h-full min-h-0 flex-col overflow-hidden">
      <header className="flex shrink-0 flex-col gap-2 border-b border-border-subtle px-4 py-3">
        <div className="flex flex-wrap items-start gap-2">
          <GitCommitHorizontal className="mt-0.5 size-4 shrink-0 text-text-secondary" />
          <h1 className="min-w-0 flex-1 break-words text-title-16">
            {rev ? (commit?.subject ?? "Commit changes") : staged ? "Staged changes" : "Working tree changes"}
          </h1>
          {commit && <CopyButton value={commit.sha} label="Copy SHA" iconOnly />}
          {!rev && (
            <IconAction
              variant="outline"
              label="Refresh"
              disabled={filesQuery.isFetching}
              icon={<RefreshCw className={cn(filesQuery.isFetching && "animate-spin")} />}
              onClick={() => {
                void filesQuery.refetch();
                void refreshGitRepo(folder, `diff:${Date.now()}`);
              }}
            />
          )}
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-body-12 text-text-secondary">
          {commit && (
            <>
              <Badge variant="secondary" className="font-mono">
                {commit.short}
              </Badge>
              <span>{commit.author}</span>
              <span>{commit.date.slice(0, 16).replace("T", " ")}</span>
            </>
          )}
          {changes.length > 0 && (
            <span className="tabular-nums text-text-tertiary">{summarize(tracked ?? [], untracked?.length ?? 0)}</span>
          )}
          {body?.task && <Badge variant="outline">Task: {body.task}</Badge>}
        </div>
        {body?.text && (
          <Collapsible>
            <CollapsibleTrigger asChild>
              <Button variant="ghost" size="sm">
                <MessageSquareText />
                Commit message
              </Button>
            </CollapsibleTrigger>
            <CollapsibleContent className="max-h-32 overflow-auto whitespace-pre-wrap break-words py-2 text-body-13 text-text-secondary">
              {body.text}
            </CollapsibleContent>
          </Collapsible>
        )}
      </header>
      {actionError && (
        <div className="px-4 py-2">
          <FeedbackBanner tone="error" title="Git action failed">
            {actionError}
          </FeedbackBanner>
        </div>
      )}
      {error && (
        <div className="px-4 py-2">
          <FeedbackBanner
            tone="error"
            title="Could not read changes"
            action={
              <Button
                variant="outline"
                onClick={() => {
                  void filesQuery.refetch();
                  if (rev) void commitQuery.refetch();
                }}
              >
                Retry
              </Button>
            }
          >
            {errorMessage(error)}
          </FeedbackBanner>
        </div>
      )}
      {loading ? (
        <div className="p-4">
          <PendingStatus>Reading changes…</PendingStatus>
        </div>
      ) : error && !filesQuery.data ? null : changes.length === 0 ? (
        error ? null : (
          <EmptyState
            icon={FileDiff}
            title="No file changes"
            body={
              rev ? "This commit has no differences against its first parent." : "There are no changes in this scope."
            }
          />
        )
      ) : (
        <>
          <div className="flex shrink-0 flex-wrap items-center gap-2 px-4 py-2">
            {!wide && (
              <Popover open={pickerOpen && visible} onOpenChange={setPickerOpen}>
                <PopoverTrigger asChild>
                  <Button variant="outline" size="sm">
                    <Files className="size-3.5" />
                    Files ({changes.length})
                  </Button>
                </PopoverTrigger>
                <PopoverContent align="start" className="h-80 w-80 max-w-[calc(100vw-32px)] p-0">
                  {fileList}
                </PopoverContent>
              </Popover>
            )}
            <span className="min-w-0 flex-1 truncate text-code-12" title={path}>
              {wide ? path : splitPath(path).name}
            </span>
            <CopyButton key={path} value={path} label="Copy path" iconOnly />
            {canMove && (
              <IconAction
                label={staged ? "Unstage file" : "Stage file"}
                icon={staged ? <Minus /> : <Plus />}
                disabled={busy}
                onClick={toggleStage}
              />
            )}
            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Previous file"
                disabled={index <= 0}
                onClick={() => move(-1)}
              >
                <ChevronLeft />
              </Button>
              <span className="text-code-11 text-text-tertiary">
                {index + 1}/{changes.length}
              </span>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Next file"
                disabled={index >= changes.length - 1}
                onClick={() => move(1)}
              >
                <ChevronRight />
              </Button>
            </div>
            <ToggleGroup
              type="single"
              value={mode}
              onValueChange={(value) => {
                if (value) gitUI.useStore.setState({ diffMode: value as DiffMode });
              }}
              size="sm"
              variant="outline"
              aria-label="Diff layout"
            >
              <ToggleGroupItem value="split" aria-label="Split view" title="Split view">
                <Columns2 />
              </ToggleGroupItem>
              <ToggleGroupItem value="unified" aria-label="Unified view" title="Unified view">
                <Rows2 />
              </ToggleGroupItem>
            </ToggleGroup>
          </div>
          <div className="min-h-0 flex-1">
            {wide ? (
              <ResizablePanelGroup orientation="horizontal">
                <ResizablePanel defaultSize="25%" minSize="180px" maxSize="40%">
                  {fileList}
                </ResizablePanel>
                <ResizableHandle withHandle />
                <ResizablePanel minSize="45%">
                  <FileDiffContent
                    key={`${rev}:${staged}:${path}`}
                    folder={folder}
                    isNew={current?.untracked === true}
                    rev={rev}
                    path={path}
                    staged={staged}
                    mode={mode}
                    before={commit?.parent ?? (rev ? "parent" : "HEAD")}
                    after={rev ? (commit?.short ?? rev.slice(0, 7)) : staged ? "Index" : "Working tree"}
                  />
                </ResizablePanel>
              </ResizablePanelGroup>
            ) : (
              <FileDiffContent
                key={`${rev}:${staged}:${path}`}
                folder={folder}
                isNew={current?.untracked === true}
                rev={rev}
                path={path}
                staged={staged}
                mode={mode}
                before={commit?.parent ?? (rev ? "parent" : "HEAD")}
                after={rev ? (commit?.short ?? rev.slice(0, 7)) : staged ? "Index" : "Working tree"}
              />
            )}
          </div>
        </>
      )}
    </div>
  );
}

/** "5 files changed · +76 −7", and the untracked files git does not count. */
function summarize(tracked: Change[], untracked: number): string {
  const extra = untracked > 0 ? `${untracked} untracked` : "";
  if (tracked.length === 0) return extra;
  return [changeSummary(tracked), extra].filter(Boolean).join(" · ");
}

function FileRow({ change, selected, onSelect }: { change: DiffEntry; selected: boolean; onSelect: () => void }) {
  const { name, dir } = splitPath(change.path);
  return (
    <li>
      <Button
        variant="ghost"
        onClick={onSelect}
        aria-current={selected ? "true" : undefined}
        title={change.path}
        className={cn("h-auto min-h-12 w-full justify-start gap-2 px-2 text-left", selected && "bg-state-soft")}
      >
        <StatusMark kind={change.untracked ? "untracked" : changeMarkKind(change.kind)} />
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-code-12">{name}</span>
          {dir && <span className="truncate text-code-11 text-text-tertiary">{dir}</span>}
        </span>
        {change.untracked ? (
          <span className="text-code-11 text-text-tertiary">new</span>
        ) : change.added < 0 ? (
          <span className="text-code-11 text-text-tertiary">binary</span>
        ) : (
          <span className="shrink-0 text-code-11 tabular-nums">
            <span className="text-status-success">+{change.added}</span>{" "}
            <span className="text-status-danger">−{change.removed}</span>
          </span>
        )}
      </Button>
    </li>
  );
}

function CopyButton({ value, label, iconOnly = false }: { value: string; label: string; iconOnly?: boolean }) {
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    setCopied(false);
    setError("");
  }, [value]);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);
  async function copy() {
    setError("");
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
    } catch {
      setError("Could not copy. Select the text to copy it.");
    }
  }
  return (
    <div className="flex flex-col">
      <Button
        variant="ghost"
        size={iconOnly ? "icon-sm" : "sm"}
        aria-label={copied ? "Copied" : label}
        title={copied ? "Copied" : label}
        onClick={() => void copy()}
      >
        {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
        {!iconOnly && (copied ? "Copied" : label)}
      </Button>
      {error && <InlineFeedback>{error}</InlineFeedback>}
    </div>
  );
}

/** One query per repository/revision/file; previous files never appear under a new label. */
function FileDiffContent({
  folder,
  isNew,
  rev,
  path,
  staged,
  mode,
  before,
  after,
}: {
  folder: string;
  /** An untracked file: there is no "before", and the patch is the whole file. */
  isNew: boolean;
  rev: string;
  path: string;
  staged: boolean;
  mode: DiffMode;
  before: string;
  after: string;
}) {
  const revision = useGitRevision((s) => s.revision);
  const [limit, setLimit] = useState(DIFF_LINES);
  const query = useQuery({
    queryKey: ["git", "patch", folder, rev, path, staged, rev ? 0 : revision],
    queryFn: () => (rev ? gitRevPatch(rev, path) : staged ? gitIndexPatch(path) : gitFilePatch(path)),
  });
  const current = useMemo(() => (query.data === undefined ? null : parsePatch(query.data)), [query.data]);
  const highlight = usePatchHighlight(current, path);
  const all = current ? patchLines(current) : [];
  const lines = all.slice(0, limit);
  const pairs = splitPairs(lines);
  return (
    <div className="flex h-full min-h-0 flex-col gap-2 overflow-auto p-3" aria-busy={query.isFetching}>
      {query.isFetching && <PendingStatus>Reading file diff…</PendingStatus>}
      {query.error && (
        <FeedbackBanner
          tone="error"
          title="Could not read file"
          action={
            <Button variant="outline" onClick={() => void query.refetch()}>
              Retry
            </Button>
          }
        >
          {errorMessage(query.error)}
        </FeedbackBanner>
      )}
      {current &&
        (current.binary || !current.hunks.length ? (
          <EmptyState
            icon={FileDiff}
            title={current.binary ? "Binary file" : "No text changes"}
            body={
              current.binary
                ? "Binary contents cannot be shown as a text diff."
                : "The file mode changed, or the diff is now empty."
            }
          />
        ) : mode === "split" ? (
          <div className="min-w-160">
            <DiffSplitPanes
              pairs={pairs}
              runs={changedRuns(pairs)}
              leftLabel={`Before · ${isNew ? "nothing" : rev ? before || "empty tree" : staged ? "HEAD" : "Index"}`}
              rightLabel={`After · ${after}`}
              segmentsFor={(line, side) =>
                side === "left" ? highlight?.before.get(line.before) : highlight?.after.get(line.after)
              }
            />
          </div>
        ) : (
          <DiffPane label={path} caps={false} meta={<Badge variant="secondary">Unified</Badge>}>
            <div>
              {lines.map((line, i) => (
                <DiffLineRow
                  key={i}
                  row={line}
                  lineNumber={(line.kind === "removed" ? line.before : line.after) + 1}
                  segments={
                    line.kind === "removed" ? highlight?.before.get(line.before) : highlight?.after.get(line.after)
                  }
                />
              ))}
            </div>
          </DiffPane>
        ))}
      {all.length > limit && (
        <div className="flex items-center gap-2 text-body-12 text-text-tertiary">
          <span>
            Showing {limit} of {all.length} diff lines
          </span>
          <Button variant="outline" onClick={() => setLimit((n) => n + DIFF_LINES)}>
            Show more
          </Button>
        </div>
      )}
    </div>
  );
}
