import { useEffect, useMemo, useState } from "react";
import {
  Play,
  FileCode2,
  BookmarkPlus,
  Download,
  Clock,
  AlertCircle,
  ShieldAlert,
  Info,
  XCircle,
  Sliders,
  Copy,
  Braces,
  Columns3,
  List,
  KeyRound,
} from "lucide-react";
import { sql as codemirrorSql, PostgreSQL, MySQL, MariaSQL } from "@codemirror/lang-sql";
import { Button } from "@/components/ui/button";
import { shortcut } from "@/lib/shortcut";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";
import { copyText } from "../../../bridge/clipboard";
import { requestVaultUnlock } from "@/workbench/vaultUnlockRequest";
import { isVaultLockedError } from "../errorStates";
import { useDatabaseStore } from "../databaseStore";
import { getEngineMeta } from "./EngineBadge";
import { explain, explainAnalyze, type QueryPlan } from "../../../bridge/database";
import { QueryPlanView } from "./QueryPlanView";

/** One column's values, newline separated — the shape that pastes into an IN
 *  list or a spreadsheet. A row shorter than the header becomes an empty line,
 *  which `join` already does for a hole; no guard needed. */
export function columnValues(rows: string[][], index: number): string {
  return rows.map((row) => row[index]).join("\n");
}

/**
 * A row is a string array beside a separate column list — JSON has to pair them
 * back up. Values arrive already stringified by the bridge, so this is a shape
 * change, not a re-encode.
 */
export function rowAsJSON(columns: string[], row: string[]): string {
  return JSON.stringify(
    Object.fromEntries(row.map((value, i) => [columns[i] || `col_${i}`, value])),
    null,
    2,
  );
}

/**
 * Figma `DB / Console Content` (742:8). A toolbar row with Run ⌘↵ and Export
 * on the right; the editor and the result each a well-surfaced card inset 24
 * from the content edge; between them the row count and the wall-clock time
 * in Body/13 Medium, with a cap chip on the right when the result was cut.
 * The table is Code/12 throughout — header on surface/raised in Medium, the
 * first column primary, the rest secondary, NULL in the warning colour.
 *
 * What the page does not draw and this keeps: Explain, Explain Analyze,
 * Save, the resizable split, and every banner. They work.
 */
export function QueryConsole() {
  const {
    profiles,
    selectedProfileId,
    sql,
    setSql,
    queryResult,
    queryDurationMs,
    runningQuery,
    queryStartTime,
    queryError,
    executeQuery,
    clearQueryError,
    schemaSnapshot,
    setInspectedCell,
    setShowExportModal,
    setShowSaveQueryModal,
    consoleEditorSize,
    setConsoleEditorSize,
  } = useDatabaseStore();

  const [elapsed, setElapsed] = useState(0);
  const [activePlan, setActivePlan] = useState<QueryPlan | null>(null);
  const [explaining, setExplaining] = useState(false);

  const profile = profiles.find((p) => p.id === selectedProfileId);
  const engineMeta = getEngineMeta(profile?.engine);

  // 30s Execution timer (Screen 14)
  useEffect(() => {
    if (!runningQuery || !queryStartTime) {
      setElapsed(0);
      return;
    }
    const interval = setInterval(() => {
      const now = Date.now();
      const diffSec = (now - queryStartTime) / 1000;
      setElapsed(diffSec);
    }, 100);
    return () => clearInterval(interval);
  }, [runningQuery, queryStartTime]);

  // CodeMirror 6 SQL Dialect & Autocomplete Extension (F2, F3)
  const sqlExtension = useMemo(() => {
    const engine = (profile?.engine || "postgres").toLowerCase();
    let dialect = PostgreSQL;
    if (engine === "mysql") {
      dialect = MySQL;
    } else if (engine === "mariadb") {
      dialect = MariaSQL;
    }

    const defaultSchema =
      engine === "postgres" ? "public" : profile?.database || "public";

    return [
      codemirrorSql({
        dialect,
        schema: schemaSnapshot?.namespace ?? undefined,
        defaultSchema,
        upperCaseKeywords: true,
      }),
    ];
  }, [profile?.engine, profile?.database, schemaSnapshot?.namespace]);

  // Check for engine placeholder mismatch for MySQL / MariaDB (Screen 21)
  const hasPlaceholderMismatch = useMemo(() => {
    const engine = (profile?.engine || "postgres").toLowerCase();
    if (engine === "mysql" || engine === "mariadb") {
      return /\$\d+/.test(sql);
    }
    return false;
  }, [profile?.engine, sql]);

  // Check if error is safe-mode / read-only rejection (Screen 18)
  const isReadOnlyRejection =
    queryError.toLowerCase().includes("read-only") ||
    queryError.includes("25006") ||
    queryError.toLowerCase().includes("transaction is read-only");

  async function handleExplain(analyze: boolean) {
    if (!selectedProfileId || !sql.trim()) return;
    setExplaining(true);
    clearQueryError();
    try {
      const plan = analyze
        ? await explainAnalyze(selectedProfileId, sql)
        : await explain(selectedProfileId, sql);
      setActivePlan(plan);
    } catch (e: any) {
      useDatabaseStore.setState({ queryError: e.message || String(e) });
    } finally {
      setExplaining(false);
    }
  }

  // Keyboard shortcut Ctrl+Enter / Cmd+Enter to run query
  function handleKeyDown(e: React.KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      void executeQuery();
    }
  }

  const busy = runningQuery || explaining || !selectedProfileId;

  return (
    <div className="flex h-full flex-1 flex-col overflow-hidden px-6 pt-3.5 pb-6" onKeyDown={handleKeyDown}>
      {/* Toolbar (742:18, 742:21): the primary Run carries its shortcut at 65%
          inside the button, Export beside it. The plan and save actions
          are DevKit's and sit on the left. */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 pb-3.5">
        <Button
          variant="outline"
          size="default"
          className="bg-surface-raised"
          onClick={() => void handleExplain(false)}
          disabled={busy}
          title="Explain query plan without executing"
        >
          <FileCode2 className="size-3.5" />
          Explain
        </Button>
        <Button
          variant="outline"
          size="default"
          className="bg-surface-raised"
          onClick={() => void handleExplain(true)}
          disabled={busy}
          title="Execute statement in rolled-back transaction to measure actual time"
        >
          <Sliders className="size-3.5" />
          Explain analyze
        </Button>
        <Button
          variant="ghost"
          size="default"
          onClick={() => setShowSaveQueryModal(true)}
          disabled={!sql.trim()}
        >
          <BookmarkPlus className="size-3.5" />
          Save
        </Button>

        <div className="flex-1" />

        {queryResult && (
          <Button
            variant="outline"
            size="lg"
            className="bg-surface-raised"
            onClick={() => setShowExportModal(true)}
          >
            <Download className="size-3.5" />
            Export
          </Button>
        )}
        <Button
          variant="default"
          size="lg"
          onClick={() => void executeQuery()}
          disabled={runningQuery || !selectedProfileId}
        >
          <Play className="size-3.5 fill-current" />
          Run
          <span className="text-code-11 opacity-65">{shortcut("Enter")}</span>
        </Button>
      </div>

      {/* The editor used to be a hard 220px. A long query and a wide result
          set want different splits, so the split is dragged, remembered, and
          keyboard-resizable — the handle is a real separator, not a bare div. */}
      <ResizablePanelGroup
        orientation="vertical"
        className="min-h-0 flex-1"
        defaultLayout={{ editor: consoleEditorSize }}
        onLayoutChanged={(layout, meta) => {
          // Only a real drag is worth writing through ui_state — the callback
          // also fires on mount and on every constraint recompute.
          if (meta.isUserInteraction) setConsoleEditorSize(layout.editor);
        }}
      >
        {/* Sizes are strings on purpose: this library reads a bare number as
            pixels, so minSize={15} would be fifteen pixels, not fifteen
            percent. */}
        <ResizablePanel id="editor" minSize="15" className="flex flex-col">
          {/* editor (742:23): a well with a subtle stroke, radius 8. */}
          <div className="relative flex-1 overflow-hidden rounded-md border border-border-subtle bg-surface-well">
            <CodeEditor value={sql} onChange={setSql} extensions={sqlExtension} wrap />
          </div>
        </ResizablePanel>

        <ResizableHandle withHandle />

        <ResizablePanel id="results" minSize="20" className="flex min-h-0 flex-col overflow-hidden">
          {/* DevKit Advice for MySQL placeholder mismatch (Screen 21) */}
          {hasPlaceholderMismatch && (
            <div className="mb-2 flex shrink-0 items-center justify-between gap-3 rounded-md border border-status-warning/30 bg-status-warning/10 px-4 py-2 text-body-12 text-status-warning">
              <div className="flex items-center gap-2">
                <Info className="size-4 shrink-0" />
                <span>
                  <strong className="font-semibold">DevKit advice:</strong> this connection uses the{" "}
                  {engineMeta.name} dialect, which takes{" "}
                  <code className="rounded bg-status-warning/20 px-1 py-0.5 font-mono">?</code>{" "}
                  for parameter placeholders rather than{" "}
                  <code className="rounded bg-status-warning/20 px-1 py-0.5 font-mono">$1</code>.
                </span>
              </div>
              <Button
                variant="ghost"
                size="xs"
                className="text-status-warning hover:bg-status-warning/20"
                onClick={() => setSql(sql.replace(/\$\d+/g, "?"))}
              >
                Auto-fix placeholders
              </Button>
            </div>
          )}

          {/* 30s Execution Timer Bar (Screen 14) */}
          {runningQuery && (
            <div className="mb-2 flex shrink-0 items-center justify-between rounded-md border border-border-subtle bg-surface-raised px-4 py-2.5 text-body-12">
              <div className="flex max-w-md flex-1 items-center gap-3">
                <div className="flex items-center gap-1.5 text-body-13-medium text-text-primary">
                  <Clock className="size-4 animate-pulse text-action-primary" />
                  <span>Executing statement…</span>
                  <span className="text-code-12 text-action-primary">{elapsed.toFixed(1)}s</span>
                </div>
                <div className="h-1.5 flex-1 overflow-hidden rounded-full border border-border">
                  <div
                    className="h-full bg-action-primary transition-all duration-100 ease-out"
                    style={{ width: `${Math.min(100, (elapsed / 30) * 100)}%` }}
                  />
                </div>
                <span className="text-caption-11 text-text-tertiary">30s cap</span>
              </div>
            </div>
          )}

          {/* Safe-Mode Rejected Banner (Screen 18) */}
          {isReadOnlyRejection && (
            <div className="mb-2 flex shrink-0 items-start gap-2.5 rounded-md border border-(--border-danger) bg-(--feedback-danger-soft) p-3 text-body-12 text-status-danger">
              <ShieldAlert className="mt-0.5 size-4 shrink-0" />
              <div className="flex-1 space-y-1">
                <div className="flex items-center gap-2 text-body-13-medium">
                  <span>Operation rejected by the database engine (safe-mode policy)</span>
                  <span className="rounded bg-status-danger/20 px-1 py-0.5 text-code-11">
                    SQLSTATE 25006
                  </span>
                </div>
                <p className="text-caption-11 text-text-secondary">
                  This connection is operating in <strong>safe-mode (read-only)</strong>. Mutating
                  queries (INSERT, UPDATE, DELETE, DDL) are blocked by the database session. You can
                  change this in Connection options.
                </p>
                <div className="mt-1 rounded border border-border bg-surface-base p-1.5 text-code-11 text-text-tertiary">
                  {queryError}
                </div>
              </div>
              <button
                type="button"
                className="p-1 text-text-tertiary hover:text-text-primary"
                onClick={clearQueryError}
              >
                <XCircle className="size-4" />
              </button>
            </div>
          )}

          {/* Vault locked mid-session (Screen 26). Deliberately a banner, not a
              replacement: the rows on screen came out of the database before the
              vault shut and are already in memory. Clearing them would destroy
              work the lock has no claim on. */}
          {isVaultLockedError(queryError) && (
            <div className="mb-2 flex shrink-0 items-center justify-between gap-3 rounded-md border border-status-warning/30 bg-status-warning/10 px-4 py-2.5 text-body-12">
              <div className="flex items-center gap-2 text-status-warning">
                <KeyRound className="size-4 shrink-0" />
                <span>
                  <strong className="font-semibold">The vault locked while you were working.</strong>{" "}
                  This result stays on screen — it is already in memory. Running another query, or
                  opening history, needs the vault key again.
                </span>
              </div>
              <Button size="sm" className="shrink-0" onClick={requestVaultUnlock}>
                Unlock vault
              </Button>
            </div>
          )}

          {/* Generic Error Banner (Screen 13) */}
          {queryError && !isReadOnlyRejection && !isVaultLockedError(queryError) && (
            <div className="mb-2 flex shrink-0 items-start gap-2.5 rounded-md border border-(--border-danger) bg-(--feedback-danger-soft) p-3 text-body-12 text-status-danger">
              <AlertCircle className="mt-0.5 size-4 shrink-0" />
              <div className="flex-1 space-y-1">
                <div className="text-body-13-medium">Query execution error</div>
                <div className="rounded border border-border bg-surface-base p-2 text-code-11 whitespace-pre-wrap text-text-primary">
                  {queryError}
                </div>
              </div>
              <button
                type="button"
                className="p-1 text-text-tertiary hover:text-text-primary"
                onClick={clearQueryError}
              >
                <XCircle className="size-4" />
              </button>
            </div>
          )}

          {queryResult ? (
            <div className="flex min-h-0 flex-1 flex-col">
              {/* 742:36–742:39: "200 rows  412 ms" and the cap chip. */}
              <div className="flex h-9 shrink-0 items-center gap-6">
                <span className="text-body-13-medium text-text-primary">
                  {queryResult.rowCount} {queryResult.rowCount === 1 ? "row" : "rows"}
                </span>
                {queryDurationMs != null && (
                  <span className="text-body-13-medium text-text-primary">{queryDurationMs} ms</span>
                )}
                <span className="text-caption-11 text-text-tertiary">
                  {queryResult.columns.length} columns
                </span>
                <span className="flex-1" />
                {queryResult.truncated && (
                  <span className="flex h-7 items-center rounded-md border border-border-subtle bg-surface-raised px-3 text-code-11 text-text-tertiary">
                    row cap reached — add LIMIT/OFFSET to page further
                  </span>
                )}
              </div>

              {/* result (743:8): a well card; header on surface/raised. */}
              <div className="min-h-0 flex-1 overflow-auto rounded-md border border-border-subtle bg-surface-well">
                <table className="w-full border-collapse text-left text-code-12" aria-label="Query results">
                  <thead className="sticky top-0 z-10 bg-surface-raised">
                    <tr>
                      {/* Keyed by index too: two columns can share a name —
                          SELECT a.id, b.id — and React would collapse them. */}
                      {queryResult.columns.map((col, j) => (
                        <ContextMenu key={`${col}-${j}`}>
                          <ContextMenuTrigger asChild>
                            <th className="h-10 max-w-[240px] truncate border-r border-b border-border-subtle px-4 text-code-12-medium text-text-secondary last:border-r-0">
                              {col}
                            </th>
                          </ContextMenuTrigger>
                          <ContextMenuContent>
                            <ContextMenuItem onSelect={() => void copyText(col || `col_${j}`)}>
                              <Columns3 />
                              Copy column name
                            </ContextMenuItem>
                            <ContextMenuItem
                              onSelect={() => void copyText(columnValues(queryResult.rows, j))}
                            >
                              <List />
                              Copy {queryResult.rows.length} value
                              {queryResult.rows.length === 1 ? "" : "s"}
                            </ContextMenuItem>
                            {/* The count is the honest one — what is loaded, not
                                what the query would have returned. Saying "copy
                                the column" over a capped result would hand
                                someone 200 rows of an unknown total. */}
                            {queryResult.truncated && (
                              <ContextMenuLabel className="text-caption-11 font-normal text-status-warning">
                                result was capped — this is not the whole column
                              </ContextMenuLabel>
                            )}
                          </ContextMenuContent>
                        </ContextMenu>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {queryResult.rows.map((row, i) => (
                      <tr key={i} className="group transition-colors hover:bg-surface-raised/60">
                        {row.map((cell, j) => {
                          const colName = queryResult.columns[j] || `col_${j}`;
                          return (
                            <ContextMenu key={j}>
                              <ContextMenuTrigger asChild>
                                <td
                                  className={cn(
                                    "h-9 max-w-[280px] cursor-pointer truncate border-r border-b border-border-subtle px-4 select-text last:border-r-0 hover:text-text-primary",
                                    j === 0 ? "text-text-primary" : "text-text-secondary",
                                  )}
                                  onClick={() =>
                                    setInspectedCell({ columnName: colName, value: cell })
                                  }
                                  title="Click to inspect cell"
                                >
                                  {cell === "" ? (
                                    <span className="text-code-11 text-status-warning">NULL</span>
                                  ) : (
                                    cell
                                  )}
                                </td>
                              </ContextMenuTrigger>
                              {/* A cell is truncated at 280px, so the value on
                                  screen is often not the whole value. Copy takes
                                  the real one. */}
                              <ContextMenuContent>
                                <ContextMenuItem onSelect={() => void copyText(cell)}>
                                  <Copy />
                                  Copy cell
                                </ContextMenuItem>
                                <ContextMenuItem
                                  onSelect={() => void copyText(rowAsJSON(queryResult.columns, row))}
                                >
                                  <Braces />
                                  Copy row as JSON
                                </ContextMenuItem>
                                <ContextMenuItem onSelect={() => void copyText(colName)}>
                                  <Columns3 />
                                  Copy column name
                                </ContextMenuItem>
                                <ContextMenuSeparator />
                                <ContextMenuItem
                                  onSelect={() =>
                                    setInspectedCell({ columnName: colName, value: cell })
                                  }
                                >
                                  Inspect cell
                                  <ContextMenuShortcut>Click</ContextMenuShortcut>
                                </ContextMenuItem>
                              </ContextMenuContent>
                            </ContextMenu>
                          );
                        })}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          ) : (
            <div className="flex flex-1 flex-col items-center justify-center rounded-md border border-border-subtle bg-surface-well p-8 text-center text-text-tertiary">
              <div className="mb-3 flex size-12 items-center justify-center rounded-control border border-border bg-surface-raised">
                <FileCode2 className="size-6 text-text-secondary" />
              </div>
              <h3 className="mb-1 text-title-14 text-text-primary">
                {selectedProfileId ? "Query console ready" : "No connection selected"}
              </h3>
              <p className="max-w-sm text-body-13 text-text-secondary">
                {selectedProfileId
                  ? `Type a SQL query above and press Run (or ${shortcut("Enter")}). Autocomplete suggests schemas and tables as you type.`
                  : "Select a connection profile in the sidebar to start writing queries."}
              </p>
            </div>
          )}
        </ResizablePanel>
      </ResizablePanelGroup>

      {/* Plan Viewer Modal (Screen 22) */}
      {activePlan && (
        <QueryPlanView plan={activePlan} onClose={() => setActivePlan(null)} />
      )}
    </div>
  );
}
