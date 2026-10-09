import { useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { CaseSensitive, ChevronDown, ChevronRight, Ellipsis, FileCode2, FolderGit2, Regex, TriangleAlert, WholeWord } from "lucide-react";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { SidebarInset } from "@/components/ui/sidebar-shell";
import { Toggle } from "@/components/ui/toggle";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useShallow } from "zustand/react/shallow";
import { useCodeGraphStore } from "../codegraphStore";
import { openFile } from "../fileActions";
import { collapseSearchGroups, runSearch, setSearchOptions, setSearchScope, toggleSearchGroup } from "../searchActions";
import { folderLabels } from "../workspace/identity";
import { scopeFolderIds, scopeSummary, toggleScopeFolder, WHOLE_WORKSPACE } from "../workspace/scope";
import {
  buildRows,
  fileGroupKey,
  previewParts,
  resultSummary,
  troubledFolders,
  type MatchRow,
  type ResultRow,
} from "../workspace/searchModel";
import { baseName, dirName, docKey } from "../workspace/identity";

const ROW_HEIGHT = 26;

/**
 * Find in workspace.
 *
 * The scope is the search's own and is always on screen above the results; it
 * starts as the sidebar's scope (or the folder a context menu was opened on) and
 * does not follow the header afterwards. Results are what Go found in the files
 * on disk — saved text — and the panel says so when unsaved edits exist. Hits are
 * grouped folder → file → match, with line and column, and only the rows on
 * screen are rendered.
 */
export function SearchTab({ workspaceId }: { workspaceId: string }) {
  const search = useCodeGraphStore((s) => s.search);
  const folders = useCodeGraphStore((s) => s.folders);
  const dirty = useCodeGraphStore(useShallow((s) => Object.values(s.buffers).filter((b) => b.dirty && b.workspaceId === workspaceId).length));
  const labels = useMemo(() => folderLabels(folders.map((f) => ({ id: f.id, name: f.name, path: f.path }))), [folders]);
  const inputRef = useRef<HTMLInputElement>(null);
  const [moreOptions, setMoreOptions] = useState(false);
  const o = search.options;

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, [search.id]);

  const rows = useMemo(() => (search.result ? buildRows(search.result, search.collapsed) : []), [search.result, search.collapsed]);
  const trouble = search.result ? troubledFolders(search.result) : [];

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <SidebarInset className="flex flex-col gap-1.5">
        <div className="relative">
          <Input
            ref={inputRef}
            value={o.query}
            onChange={(e) => setSearchOptions({ query: e.target.value })}
            onKeyDown={(e) => {
              if (e.key === "Enter") void runSearch();
            }}
            placeholder="Find in files"
            aria-label="Find in workspace"
            aria-invalid={search.status === "invalid"}
            className="h-(--control-height-md) border-border-subtle bg-surface-well pr-20 text-body-13"
          />
          <div className="absolute top-1/2 right-1 flex -translate-y-1/2 items-center gap-0.5">
            <OptionToggle label="Match case" pressed={o.caseSensitive} onPressedChange={(v) => setSearchOptions({ caseSensitive: v })}>
              <CaseSensitive />
            </OptionToggle>
            <OptionToggle label="Match whole word" pressed={o.wholeWord} onPressedChange={(v) => setSearchOptions({ wholeWord: v })}>
              <WholeWord />
            </OptionToggle>
            <OptionToggle label="Use regular expression" pressed={o.regex} onPressedChange={(v) => setSearchOptions({ regex: v })}>
              <Regex />
            </OptionToggle>
          </div>
        </div>

        <div className="flex items-center gap-1.5">
          <SearchScopePicker folders={folders} labels={labels} />
          <Tooltip>
            <TooltipTrigger asChild>
              <Toggle size="sm" pressed={moreOptions} onPressedChange={setMoreOptions} aria-label="Files to include or exclude" className="size-6 min-w-6 px-0">
                <Ellipsis />
              </Toggle>
            </TooltipTrigger>
            <TooltipContent>Files to include / exclude</TooltipContent>
          </Tooltip>
        </div>

        {moreOptions ? (
          <div className="flex flex-col gap-1.5">
            <Input
              value={o.include}
              onChange={(e) => setSearchOptions({ include: e.target.value })}
              placeholder="Include: src/**, *.ts"
              aria-label="Files to include"
              className="h-(--control-height-sm) border-border-subtle bg-surface-well text-code-11"
            />
            <Input
              value={o.exclude}
              onChange={(e) => setSearchOptions({ exclude: e.target.value })}
              placeholder="Exclude: **/generated/**, *.min.js"
              aria-label="Files to exclude"
              className="h-(--control-height-sm) border-border-subtle bg-surface-well text-code-11"
            />
            <p className="text-caption-11 text-text-tertiary">
              Paths are relative to each folder. <code>*.ts</code> matches at any depth; <code>src/**</code> is anchored at the folder; a trailing / means a
              directory. Exclude wins.
            </p>
          </div>
        ) : null}
      </SidebarInset>

      <SidebarInset className="flex flex-col gap-1">
        <StatusLine />
        {dirty > 0 ? (
          <p className="text-caption-11 text-text-tertiary">
            Searches the files on disk. {dirty} file{dirty === 1 ? " has" : "s have"} unsaved edits, which are not included.
          </p>
        ) : (
          <p className="text-caption-11 text-text-tertiary">Searches the files on disk.</p>
        )}
        {trouble.map((t) => (
          <p key={t.folderId} className="flex items-center gap-1 text-caption-11 text-status-warning">
            <TriangleAlert className="size-3 shrink-0" aria-hidden />
            <span className="truncate">
              {labels[t.folderId] ?? "A folder"} was not searched{t.error ? `: ${t.error}` : ""}
            </span>
          </p>
        ))}
        {search.result && rows.length > 0 ? (
          <div className="flex items-center justify-end gap-2 text-caption-11 text-text-tertiary">
            <button type="button" className="hover:text-text-primary" onClick={() => collapseSearchGroups([])}>Expand all</button>
            <button
              type="button"
              className="hover:text-text-primary"
              onClick={() => collapseSearchGroups(search.result!.files.map((f) => fileGroupKey(f.folderId, f.path)))}
            >
              Collapse files
            </button>
          </div>
        ) : null}
      </SidebarInset>

      <Results rows={rows} labels={labels} workspaceId={workspaceId} identifier={o.query} stale={search.status === "searching"} />
    </div>
  );
}

function OptionToggle({ label, pressed, onPressedChange, children }: { label: string; pressed: boolean; onPressedChange: (v: boolean) => void; children: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Toggle size="sm" pressed={pressed} onPressedChange={onPressedChange} aria-label={label} className="size-6 min-w-6 px-0">
          {children}
        </Toggle>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/** The search's scope, always visible, and changeable without touching the sidebar's. */
function SearchScopePicker({ folders, labels }: { folders: Array<{ id: string; name: string; path: string }>; labels: Record<string, string> }) {
  const scope = useCodeGraphStore((s) => s.search.scope);
  const covered = new Set(scopeFolderIds(scope, folders));
  const summary = scopeSummary(scope, folders as never, labels);
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={`Search scope: ${summary}`}
          className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md border border-border-subtle bg-surface-raised px-2 py-1 text-left text-code-11 text-text-secondary hover:bg-state-soft"
        >
          <FolderGit2 className="size-3 shrink-0" aria-hidden />
          <span className="min-w-0 flex-1 truncate" title={summary}>{summary}</span>
          <ChevronDown className="size-3 shrink-0 text-text-tertiary" aria-hidden />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 gap-1 p-1.5">
        <button type="button" role="radio" aria-checked={scope.kind === "all"} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-body-13 hover:bg-state-soft" onClick={() => setSearchScope(WHOLE_WORKSPACE)}>
          <span className="w-4 text-center">{scope.kind === "all" ? "✓" : ""}</span>
          Whole workspace
        </button>
        <div className="max-h-64 overflow-y-auto" role="group" aria-label="Folders to search">
          {folders.map((f) => (
            <label key={f.id} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-state-soft">
              <Checkbox checked={covered.has(f.id)} onCheckedChange={() => setSearchScope(toggleScopeFolder(scope, f.id, folders))} aria-label={labels[f.id]} />
              <span className="min-w-0 flex-1 truncate text-body-13">{labels[f.id]}</span>
            </label>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function StatusLine() {
  const status = useCodeGraphStore((s) => s.search.status);
  const error = useCodeGraphStore((s) => s.search.error);
  const result = useCodeGraphStore((s) => s.search.result);
  const query = useCodeGraphStore((s) => s.search.options.query);
  if (!query) return <p className="text-caption-11 text-text-tertiary">Type to search every file in the folders above.</p>;
  if (status === "invalid") return <p role="alert" className="text-caption-11 text-status-danger">{error}</p>;
  if (status === "failed") return <p role="alert" className="text-caption-11 text-status-danger">Search failed: {error}</p>;
  if (status === "searching") return <p role="status" className="text-caption-11 text-text-tertiary">Searching…{result ? " (showing the previous results)" : ""}</p>;
  if (result) {
    return (
      <p role="status" className="text-caption-11 text-text-secondary">
        {result.matches === 0 ? `No matches · ${result.filesSearched} files searched` : resultSummary(result)}
      </p>
    );
  }
  return null;
}

function Results({ rows, labels, workspaceId, identifier, stale }: { rows: ResultRow[]; labels: Record<string, string>; workspaceId: string; identifier: string; stale: boolean }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState(0);
  const virtual = useVirtualizer({ count: rows.length, getScrollElement: () => scrollRef.current, estimateSize: () => ROW_HEIGHT, overscan: 12 });

  useEffect(() => setActive(0), [rows.length === 0]);

  const open = (row: MatchRow | Extract<ResultRow, { kind: "file" }>, options: { keep?: boolean; beside?: boolean }) => {
    const line = row.kind === "match" ? row.match.line : null;
    const column = row.kind === "match" ? row.match.column : undefined;
    void openFile({ workspaceId, folderId: row.folderId, path: row.path }, line, { preview: !options.keep && !options.beside, beside: options.beside, column });
  };
  const activate = (row: ResultRow, e: { metaKey: boolean; ctrlKey: boolean; detail?: number }) => {
    if (row.kind === "folder") toggleSearchGroup(row.key);
    else if (row.kind === "file") toggleSearchGroup(row.key);
    else open(row, { keep: (e.detail ?? 1) > 1, beside: e.metaKey || e.ctrlKey });
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const next = Math.max(0, Math.min(rows.length - 1, active + (e.key === "ArrowDown" ? 1 : -1)));
      setActive(next);
      virtual.scrollToIndex(next);
    } else if (e.key === "Enter" && rows[active]) {
      e.preventDefault();
      const row = rows[active];
      if (row.kind === "match") open(row, { keep: true, beside: e.metaKey || e.ctrlKey });
      else toggleSearchGroup(row.key);
    } else if ((e.key === "ArrowLeft" || e.key === "ArrowRight") && rows[active] && rows[active].kind !== "match") {
      e.preventDefault();
      const row = rows[active];
      const closed = row.collapsed;
      if ((e.key === "ArrowLeft" && !closed) || (e.key === "ArrowRight" && closed)) toggleSearchGroup(row.key);
    }
  };

  if (rows.length === 0) {
    return <div className="min-h-0 flex-1" />;
  }
  return (
    <div
      ref={scrollRef}
      role="tree"
      aria-label="Search results"
      tabIndex={0}
      onKeyDown={onKeyDown}
      className={cn("min-h-0 flex-1 overflow-y-auto outline-none scrollbar-gutter-stable", stale && "opacity-60")}
    >
      <div style={{ height: virtual.getTotalSize(), position: "relative" }}>
        {virtual.getVirtualItems().map((item) => {
          const row = rows[item.index];
          return (
            <div
              key={row.key}
              style={{ position: "absolute", top: 0, left: 0, width: "100%", height: ROW_HEIGHT, transform: `translateY(${item.start}px)` }}
              role="treeitem"
              aria-level={row.kind === "folder" ? 1 : row.kind === "file" ? 2 : 3}
              aria-selected={item.index === active}
              aria-expanded={row.kind === "match" ? undefined : !row.collapsed}
            >
              <ResultRowView row={row} active={item.index === active} labels={labels} identifier={identifier} onActivate={(e) => { setActive(item.index); activate(row, e); }} />
            </div>
          );
        })}
      </div>
    </div>
  );
}

function ResultRowView({ row, active, labels, identifier, onActivate }: { row: ResultRow; active: boolean; labels: Record<string, string>; identifier: string; onActivate: (e: React.MouseEvent) => void }) {
  void identifier;
  const base = cn("flex h-full w-full items-center gap-1.5 text-left", active ? "bg-surface-raised" : "hover:bg-state-soft");
  if (row.kind === "folder") {
    return (
      <button type="button" onClick={onActivate} className={cn(base, "px-2")} title={labels[row.folderId]}>
        {row.collapsed ? <ChevronRight className="size-3 shrink-0" aria-hidden /> : <ChevronDown className="size-3 shrink-0" aria-hidden />}
        <FolderGit2 className="size-3.5 shrink-0 text-text-secondary" aria-hidden />
        <span className="min-w-0 flex-1 truncate text-title-13 text-text-primary">{labels[row.folderId] ?? "Folder"}</span>
        {row.state !== "ok" ? (
          <span className="shrink-0 text-code-11 text-status-warning">{row.state === "unavailable" ? "unavailable" : row.state === "duplicate" ? "duplicate" : "error"}</span>
        ) : (
          <span className="shrink-0 text-code-11 text-text-tertiary">{row.matches}</span>
        )}
      </button>
    );
  }
  if (row.kind === "file") {
    return (
      <button type="button" onClick={onActivate} className={cn(base, "pl-5 pr-2")} title={`${labels[row.folderId] ?? ""} › ${row.path}`}>
        {row.collapsed ? <ChevronRight className="size-3 shrink-0" aria-hidden /> : <ChevronDown className="size-3 shrink-0" aria-hidden />}
        <FileCode2 className="size-3.5 shrink-0 text-text-secondary" aria-hidden />
        <span className="shrink-0 text-body-13 text-text-primary">{baseName(row.path)}</span>
        <span className="min-w-0 flex-1 truncate text-code-11 text-text-tertiary">
          {dirName(row.path)}
        </span>
        <span className="shrink-0 text-code-11 text-text-tertiary">{row.matches}{row.truncated ? "+" : ""}</span>
      </button>
    );
  }
  const { before, hit, after } = previewParts(row.match);
  return (
    <button
      type="button"
      onClick={onActivate}
      className={cn(base, "pl-10 pr-2")}
      title={`${row.path}:${row.match.line}:${row.match.column}`}
      data-key={docKey("", row.folderId, row.path)}
    >
      <span className="w-14 shrink-0 text-right text-code-11 tabular-nums text-text-tertiary">
        {row.match.line}:{row.match.column}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-code-11 text-text-secondary">
        {before.trimStart()}
        <mark className="rounded-sm bg-state-soft px-0.5 text-text-primary">{hit}</mark>
        {after}
      </span>
    </button>
  );
}

