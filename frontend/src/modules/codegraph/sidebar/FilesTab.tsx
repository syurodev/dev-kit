import { useEffect, useMemo, useState } from "react";
import { cn } from "@/lib/utils";
import { getCodeGraphBlastRadius, type CodeGraphFileEntry, type CodeGraphGitState } from "@/bridge/codegraph";
import { FileTree } from "../FileTree";
import { filterFileEntries } from "../fileSearch";

const TOGGLE = "inline-flex h-5.5 shrink-0 items-center rounded-sm border px-2 text-code-11";

const SOURCE = /\.(java|kt|groovy|scala)$/i;

const GIT_WORD: Record<CodeGraphGitState, string> = { M: "Modified", A: "New file", D: "Deleted", R: "Renamed" };

export type FileFilter = { javaOnly: boolean; changed: boolean; inFlow: boolean };

/**
 * The project's files (Figma `CodeGraph / Sidebar — 5 Files`): three toggles
 * over the tree — sources only, changed only, only what the flow on screen
 * passes through — git marks on each row, and under it what the selected file
 * touches.
 */
export function FilesTab({
  files,
  query,
  selectedFile,
  gitStatus,
  flowFiles,
  filter,
  onFilter,
  onSelectFile,
}: {
  files: CodeGraphFileEntry[];
  query: string;
  selectedFile: string | null;
  gitStatus: Record<string, CodeGraphGitState>;
  /** Files of the nodes in the graph on screen. */
  flowFiles: ReadonlySet<string>;
  filter: FileFilter;
  onFilter: (next: FileFilter) => void;
  onSelectFile: (path: string) => void;
}) {
  const changedCount = Object.keys(gitStatus).length;
  const keepFile = useMemo(() => {
    if (!filter.javaOnly && !filter.changed && !filter.inFlow) return undefined;
    return (path: string) =>
      (!filter.javaOnly || SOURCE.test(path)) &&
      (!filter.changed || path in gitStatus) &&
      (!filter.inFlow || flowFiles.has(path));
  }, [filter, gitStatus, flowFiles]);
  const shown = useMemo(() => filterFileEntries(files, query, keepFile), [files, query, keepFile]);

  const toggle = (key: keyof FileFilter, label: string, disabled = false) => (
    <button
      type="button"
      aria-pressed={filter[key]}
      disabled={disabled}
      onClick={() => onFilter({ ...filter, [key]: !filter[key] })}
      className={cn(
        TOGGLE,
        filter[key]
          ? "border-(--border-default) bg-surface-raised text-text-primary"
          : "border-border-subtle text-text-tertiary hover:text-text-secondary",
        "disabled:opacity-50",
      )}
    >
      {label}
    </button>
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex flex-wrap items-center gap-1" role="group" aria-label="Filter files">
        {toggle("javaOnly", "JAVA ONLY")}
        {toggle("changed", changedCount > 0 ? `CHANGED · ${changedCount}` : "CHANGED")}
        {toggle("inFlow", "IN FLOW", flowFiles.size === 0 && !filter.inFlow)}
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        {shown.entries.length > 0 ? (
          <FileTree
            entries={shown.entries}
            forceExpanded={shown.forceExpanded}
            selectedPath={selectedFile}
            onSelectFile={onSelectFile}
            marks={gitStatus}
          />
        ) : (
          <p className="px-2 py-3 text-center text-body-12 text-text-tertiary">
            {files.length === 0 ? "No files yet — open a project." : "No files match."}
          </p>
        )}
      </div>
      {selectedFile ? (
        <FileContext path={selectedFile} git={gitStatus[selectedFile]} inFlow={flowFiles.has(selectedFile)} />
      ) : null}
    </div>
  );
}

/**
 * What the selected file reaches: whether the flow on screen passes through
 * it, and how many files depend on it — the blast radius of an edit.
 */
function FileContext({ path, git, inFlow }: { path: string; git?: CodeGraphGitState; inFlow: boolean }) {
  const [dependents, setDependents] = useState<number | null>(null);

  useEffect(() => {
    let live = true;
    setDependents(null);
    // After a pause, not on every arrow key through the tree: it walks a
    // transitive closure.
    const t = window.setTimeout(() => {
      getCodeGraphBlastRadius(path)
        .then((files) => live && setDependents(files.length))
        // Unknown is not "nothing": a failed read just says less.
        .catch(() => live && setDependents(-1));
    }, 250);
    return () => {
      live = false;
      window.clearTimeout(t);
    };
  }, [path]);

  const parts = [
    git ? GIT_WORD[git] : null,
    inFlow ? "In the flow on screen" : null,
    dependents === null || dependents < 0 ? null : dependents === 0 ? "Nothing depends on it" : `${dependents} ${dependents === 1 ? "file depends" : "files depend"} on it`,
  ].filter(Boolean);

  return (
    <div className="flex shrink-0 flex-col gap-1 rounded-lg border border-border-subtle bg-surface-raised px-2.5 py-2">
      <span className="truncate text-caption-11-medium text-text-primary" title={path}>
        {path.slice(path.lastIndexOf("/") + 1)}
      </span>
      <span className="text-caption-11 text-text-tertiary">{parts.length > 0 ? parts.join(" · ") : dependents === null ? "Working out what depends on it…" : "Unchanged"}</span>
    </div>
  );
}
