import { useMemo } from "react";
import { cn } from "@/lib/utils";
import type { CodeGraphFileEntry, CodeGraphGitState } from "@/bridge/codegraph";
import { FileCode2, Folder, FolderCode, Package } from "lucide-react";
import { TreeView, type TreeNodeData } from "@/components/tree-view";
import { filterFileEntries } from "../fileSearch";
import { compactFiles, isSourceRoot, nestFiles, type FileBranch } from "./treeNodes";

const TOGGLE = "inline-flex h-5.5 shrink-0 items-center rounded-sm border px-2 text-code-11";

/**
 * Source code in any language a project is likely to mix — a folder of
 * microservices is Java beside Go beside TypeScript, and the analyzer will
 * read more than Java. What it hides is what is not code: build files,
 * config, resources, lockfiles, docs.
 */
const CODE =
  /\.(java|kt|kts|groovy|scala|go|py|ts|tsx|js|jsx|mjs|cjs|rs|rb|php|cs|fs|swift|m|mm|c|h|cc|cpp|hpp|dart|ex|exs|erl|clj|lua|sql|proto|graphql|vue|svelte|sh)$/i;

const MARK_TONE: Record<CodeGraphGitState, string> = {
  M: "text-status-warning",
  A: "text-status-success",
  D: "text-status-danger",
  R: "text-status-info",
};


export type FileFilter = { codeOnly: boolean; changed: boolean; inFlow: boolean };

/**
 * The project's files (Figma `CodeGraph / Sidebar — 5 Files`): three toggles
 * over the tree — code only, changed only, only what the flow on screen
 * passes through — and git marks on each row. What depends on a file is the
 * Impact panel under its editor, not a card here.
 */
export function FilesTab({
  files,
  query,
  selectedFile,
  unsaved,
  gitStatus,
  flowFiles,
  filter,
  onFilter,
  expanded,
  onToggleFolder,
  onSelectFile,
  onKeepFile,
}: {
  files: CodeGraphFileEntry[];
  query: string;
  selectedFile: string | null;
  /** Files with edits not yet saved — open or not. */
  unsaved: ReadonlySet<string>;
  gitStatus: Record<string, CodeGraphGitState>;
  /** Files of the nodes in the graph on screen. */
  flowFiles: ReadonlySet<string>;
  filter: FileFilter;
  onFilter: (next: FileFilter) => void;
  /** Folders the user opened. A search opens the ones holding its matches too. */
  expanded: string[];
  onToggleFolder: (path: string) => void;
  onSelectFile: (path: string) => void;
  onKeepFile: (path: string) => void;
}) {
  const changedCount = Object.keys(gitStatus).length;
  const keepFile = useMemo(() => {
    if (!filter.codeOnly && !filter.changed && !filter.inFlow) return undefined;
    return (path: string) =>
      (!filter.codeOnly || CODE.test(path)) &&
      (!filter.changed || path in gitStatus) &&
      (!filter.inFlow || flowFiles.has(path));
  }, [filter, gitStatus, flowFiles]);
  const shown = useMemo(() => filterFileEntries(files, query, keepFile), [files, query, keepFile]);
  const nodes = useMemo(() => {
    const open = new Set([...expanded, ...(shown.forceExpanded ?? [])]);
    const toNode = (b: FileBranch): TreeNodeData =>
      b.isDir
        ? {
            id: b.path,
            label: b.label ?? b.name,
            title: b.path,
            icon: b.isPackage ? <Package /> : isSourceRoot(b.path) ? <FolderCode /> : <Folder />,
            expandable: b.children.length > 0,
            expanded: open.has(b.path),
            onToggle: () => onToggleFolder(b.path),
            children: b.children.map(toNode),
          }
        : {
            id: b.path,
            label: b.name,
            title: b.path,
            icon: <FileCode2 />,
            selected: b.path === selectedFile,
            onSelect: () => onSelectFile(b.path),
            onActivate: () => onKeepFile(b.path),
            trailing:
              unsaved.has(b.path) || gitStatus[b.path] ? (
                <span className="flex items-center gap-1.5">
                  {unsaved.has(b.path) ? (
                    <span aria-label="unsaved" title="Unsaved edits" className="text-text-primary">
                      ●
                    </span>
                  ) : null}
                  {gitStatus[b.path] ? <span className={MARK_TONE[gitStatus[b.path]]}>{gitStatus[b.path]}</span> : null}
                </span>
              ) : null,
          };
    return compactFiles(nestFiles(shown.entries)).map(toNode);
  }, [shown, expanded, selectedFile, unsaved, gitStatus, onToggleFolder, onSelectFile, onKeepFile]);

  const toggle = (key: keyof FileFilter, label: string, hint: string, disabled = false) => (
    <button
      type="button"
      title={hint}
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
        {toggle("codeOnly", "CODE", "Source code only, in any language — hides build files, config, resources and docs")}
        {toggle("changed", changedCount > 0 ? `CHANGED · ${changedCount}` : "CHANGED", "Files with uncommitted changes (git status)")}
        {toggle("inFlow", "IN FLOW", "Files the flow in front passes through", flowFiles.size === 0 && !filter.inFlow)}
      </div>
      {nodes.length > 0 ? (
        <TreeView nodes={nodes} className="min-h-0 flex-1 scrollbar-gutter-stable" />
      ) : (
        <p className="min-h-0 flex-1 px-2 py-3 text-center text-body-12 text-text-tertiary">
          {files.length === 0 ? "No files yet — open a project." : "No files match."}
        </p>
      )}
    </div>
  );
}

