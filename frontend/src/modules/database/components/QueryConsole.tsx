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
  CheckCircle2,
  Sliders,
  Table as TableIcon,
  Copy,
  Braces,
  Columns3,
  List,
  KeyRound
} from "lucide-react";
import { sql as codemirrorSql, PostgreSQL, MySQL, MariaSQL } from "@codemirror/lang-sql";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { shortcut } from "@/lib/shortcut";
import { Badge } from "@/components/ui/badge";
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
import { copyText } from "../../../bridge/clipboard";
import { requestVaultUnlock } from "@/workbench/vaultUnlockRequest";
import { isVaultLockedError } from "../errorStates";
import { useDatabaseStore } from "../databaseStore";
import { EngineBadge, getEngineMeta } from "./EngineBadge";
import { explain, explainAnalyze, type QueryPlan } from "../../../bridge/database";
import { QueryPlanView } from "./QueryPlanView";

/**
 * A row is a string array beside a separate column list — JSON has to pair them
 * back up. Values arrive already stringified by the bridge, so this is a shape
 * change, not a re-encode.
 */
/** One column's values, newline separated — the shape that pastes into an IN
 *  list or a spreadsheet. A row shorter than the header becomes an empty line,
 *  which `join` already does for a hole; no guard needed. */
export function columnValues(rows: string[][], index: number): string {
  return rows.map((row) => row[index]).join("\n");
}

export function rowAsJSON(columns: string[], row: string[]): string {
  return JSON.stringify(
    Object.fromEntries(row.map((value, i) => [columns[i] || `col_${i}`, value])),
    null,
    2,
  );
}

export function QueryConsole() {
  const {
    profiles,
    selectedProfileId,
    sql,
    setSql,
    queryResult,
    runningQuery,
    queryStartTime,
    queryError,
    queryStatus,
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

  return (
    <div className="flex-1 flex flex-col h-full overflow-hidden" onKeyDown={handleKeyDown}>
      {/* Editor Section */}
      {/* The editor used to be a hard 220px. A long query and a wide result
          set want different splits, so the split is dragged, remembered, and
          keyboard-resizable — the handle is a real separator, not a bare div. */}
      <ResizablePanelGroup
        orientation="vertical"
        className="flex-1 min-h-0"
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
      <ResizablePanel
        id="editor"
        minSize="15"
        className="flex flex-col border-b border-border"
      >
        {/* Editor Toolbar */}
        <div className="flex flex-wrap items-center justify-between px-3 py-1.5 border-b border-border/60 bg-surface-raised/40 gap-2">
          <div className="flex items-center gap-2">
            <Button
              variant="default"
              size="sm"
              className="h-7 text-label-12 gap-1.5 shadow-xs"
              onClick={() => void executeQuery()}
              disabled={runningQuery || !selectedProfileId}
            >
              <Play className="w-3.5 h-3.5 fill-current" />
              <span>Run</span>
              {/* Kbd's default muted surface is meant for neutral chrome; on the
                  accent-filled primary button it needs to read against lime, so
                  it borrows the button's own foreground. */}
              <Kbd className="ml-0.5 hidden bg-black/12 text-current sm:inline-flex">
                {shortcut("Enter")}
              </Kbd>
            </Button>

            <Button
              variant="outline"
              size="sm"
              className="h-7 text-label-12 gap-1 text-text-secondary"
              onClick={() => void handleExplain(false)}
              disabled={runningQuery || explaining || !selectedProfileId}
              title="Explain query plan without executing"
            >
              <FileCode2 className="w-3 h-3" />
              <span>Explain</span>
            </Button>

            <Button
              variant="outline"
              size="sm"
              className="h-7 text-label-12 gap-1 text-text-secondary"
              onClick={() => void handleExplain(true)}
              disabled={runningQuery || explaining || !selectedProfileId}
              title="Execute statement in rolled-back transaction to measure actual time"
            >
              <Sliders className="w-3 h-3" />
              <span>Explain Analyze</span>
            </Button>

            <Button
              variant="ghost"
              size="sm"
              className="h-7 text-label-12 gap-1 text-text-secondary hover:text-text-primary"
              onClick={() => setShowSaveQueryModal(true)}
              disabled={!sql.trim()}
            >
              <BookmarkPlus className="w-3.5 h-3.5" />
              <span>Save</span>
            </Button>
          </div>

          <div className="flex items-center gap-2">
            <EngineBadge engine={profile?.engine} showPlaceholder />
            {queryResult && (
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-label-12 gap-1 text-text-secondary"
                onClick={() => setShowExportModal(true)}
              >
                <Download className="w-3.5 h-3.5" />
                <span>Export</span>
              </Button>
            )}
          </div>
        </div>

        {/* CodeMirror Editor Area */}
        <div className="flex-1 relative overflow-hidden">
          <CodeEditor
            value={sql}
            onChange={setSql}
            extensions={sqlExtension}
            wrap
          />
        </div>
      </ResizablePanel>

      <ResizableHandle withHandle />

      <ResizablePanel id="results" minSize="20" className="flex flex-col min-h-0 overflow-hidden">

      {/* DevKit Advice for MySQL placeholder mismatch (Screen 21) */}
      {hasPlaceholderMismatch && (
        <div className="bg-amber-500/10 border-b border-amber-500/30 px-4 py-2 flex items-center justify-between text-body-12 text-amber-500 shrink-0">
          <div className="flex items-center gap-2">
            <Info className="w-4 h-4 shrink-0" />
            <span>
              <strong className="font-semibold">DevKit Advice:</strong> This connection uses{" "}
              {engineMeta.name} dialect which requires <code className="bg-amber-500/20 px-1 py-0.5 rounded font-mono">?</code> for parameter placeholders rather than <code className="bg-amber-500/20 px-1 py-0.5 rounded font-mono">$1</code>.
            </span>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="h-6 text-caption-11 text-amber-500 hover:bg-amber-500/20"
            onClick={() => setSql(sql.replace(/\$\d+/g, "?"))}
          >
            Auto-fix placeholders
          </Button>
        </div>
      )}

      {/* 30s Execution Timer Bar (Screen 14) */}
      {runningQuery && (
        <div className="bg-surface-raised border-b border-border px-4 py-2.5 flex items-center justify-between text-body-12 shrink-0">
          <div className="flex items-center gap-3 flex-1 max-w-md">
            <div className="flex items-center gap-1.5 text-text-primary font-medium">
              <Clock className="w-4 h-4 text-action-primary animate-pulse" />
              <span>Executing statement…</span>
              <span className="font-mono text-action-primary">{elapsed.toFixed(1)}s</span>
            </div>
            <div className="flex-1 h-1.5 rounded-full overflow-hidden border border-border">
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
        <div className="bg-status-danger/10 border-b border-status-danger/30 p-3 flex items-start gap-2.5 text-body-12 text-status-danger shrink-0">
          <ShieldAlert className="w-4 h-4 shrink-0 mt-0.5" />
          <div className="flex-1 space-y-1">
            <div className="font-semibold flex items-center gap-2">
              <span>Operation Rejected by Database Engine (Safe-Mode Policy)</span>
              <span className="text-code-11 bg-status-danger/20 px-1 py-0.5 rounded">
                SQLSTATE 25006
              </span>
            </div>
            <p className="text-text-secondary text-caption-11">
              This connection is operating in <strong>Safe-Mode (Read-Only)</strong>. Mutating queries (INSERT, UPDATE, DELETE, DDL) are blocked by the database session. You can change this setting in Connection Options.
            </p>
            <div className="text-code-11 text-text-tertiary bg-surface-base p-1.5 rounded border border-border mt-1">
              {queryError}
            </div>
          </div>
          <button
            type="button"
            className="text-text-tertiary hover:text-text-primary p-1"
            onClick={clearQueryError}
          >
            <XCircle className="w-4 h-4" />
          </button>
        </div>
      )}

      {/* Vault locked mid-session (Screen 26). Deliberately a banner, not a
          replacement: the rows on screen came out of the database before the
          vault shut and are already in memory. Clearing them would destroy
          work the lock has no claim on. */}
      {isVaultLockedError(queryError) && (
        <div className="bg-status-warning/10 border-b border-status-warning/30 px-4 py-2.5 flex items-center justify-between gap-3 text-body-12 shrink-0">
          <div className="flex items-center gap-2 text-status-warning">
            <KeyRound className="w-4 h-4 shrink-0" />
            <span>
              <strong className="font-semibold">The vault locked while you were working.</strong>{" "}
              This result stays on screen — it is already in memory. Running another query, or
              opening history, needs the vault key again.
            </span>
          </div>
          <Button size="sm" className="h-7 text-label-12 shrink-0" onClick={requestVaultUnlock}>
            Unlock vault
          </Button>
        </div>
      )}

      {/* Generic Error Banner (Screen 13) */}
      {queryError && !isReadOnlyRejection && !isVaultLockedError(queryError) && (
        <div className="bg-status-danger/10 border-b border-status-danger/30 p-3 flex items-start gap-2.5 text-body-12 text-status-danger shrink-0">
          <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
          <div className="flex-1 space-y-1">
            <div className="font-semibold">Query Execution Error</div>
            <div className="text-code-11 text-text-primary bg-surface-base p-2 rounded border border-border whitespace-pre-wrap">
              {queryError}
            </div>
          </div>
          <button
            type="button"
            className="text-text-tertiary hover:text-text-primary p-1"
            onClick={clearQueryError}
          >
            <XCircle className="w-4 h-4" />
          </button>
        </div>
      )}

      {/* Query Status */}
      {queryStatus && !queryError && (
        <div className="bg-status-success/10 border-b border-status-success/20 px-4 py-1.5 flex items-center justify-between text-body-12 text-status-success shrink-0">
          <span className="flex items-center gap-1.5 font-medium">
            <CheckCircle2 className="w-3.5 h-3.5" />
            {queryStatus}
          </span>
          <span className="text-caption-11 text-text-tertiary">
            Double-click any cell to inspect its content
          </span>
        </div>
      )}

      {/* Main Content Area: Result Table or Empty state */}
      <div className="flex-1 overflow-auto flex flex-col">
        {queryResult ? (
          <div className="flex flex-col h-full">
            {/* Table Meta Bar */}
            <div className="px-4 py-2 border-b border-border flex items-center justify-between text-body-12 bg-surface-raised/30 shrink-0">
              <div className="flex items-center gap-2 font-medium text-text-primary">
                <TableIcon className="w-3.5 h-3.5 text-text-secondary" />
                <span>{queryResult.rowCount} row(s)</span>
                {queryResult.truncated && (
                  <Badge variant="secondary" className="text-caption-11 bg-amber-500/15 text-amber-500 border-amber-500/30">
                    Capped at 200 rows
                  </Badge>
                )}
              </div>
              <div className="flex items-center gap-2 text-text-tertiary text-caption-11">
                <span>{queryResult.columns.length} columns</span>
              </div>
            </div>

            {/* Virtualized/Scrollable Table */}
            <div className="flex-1 overflow-auto">
              <table className="w-full text-left text-code-12 border-collapse" aria-label="Query results">
                <thead className="bg-surface-raised sticky top-0 z-10 border-b border-border shadow-2xs">
                  <tr>
                    <th className="w-12 px-3 py-2 text-center text-text-tertiary font-semibold border-r border-border/40 select-none bg-surface-raised">
                      #
                    </th>
                    {/* Keyed by index too: two columns can share a name —
                        SELECT a.id, b.id — and React would collapse them. */}
                    {queryResult.columns.map((col, j) => (
                      <ContextMenu key={`${col}-${j}`}>
                        <ContextMenuTrigger asChild>
                          <th className="px-3 py-2 text-text-primary font-semibold border-r border-border/40 bg-surface-raised truncate max-w-[240px]">
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
                <tbody className="divide-y divide-border/40 text-text-primary">
                  {queryResult.rows.map((row, i) => (
                    <tr
                      key={i}
                      className="hover:bg-surface-raised/60 transition-colors group"
                    >
                      <td className="w-12 px-3 py-2 text-center text-text-tertiary text-caption-11 border-r border-border/40 select-none bg-surface-base group-hover:bg-surface-raised/40">
                        {i + 1}
                      </td>
                      {row.map((cell, j) => {
                        const colName = queryResult.columns[j] || `col_${j}`;
                        return (
                          <ContextMenu key={j}>
                            <ContextMenuTrigger asChild>
                              <td
                                className="px-3 py-2 text-text-secondary border-r border-border/40 truncate max-w-[280px] cursor-pointer hover:text-text-primary hover:bg-action-primary/10 select-text"
                                onClick={() =>
                                  setInspectedCell({ columnName: colName, value: cell })
                                }
                                title="Click to inspect cell"
                              >
                                {cell === "" ? (
                                  <span className="text-text-tertiary italic text-caption-11">
                                    NULL
                                  </span>
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
          <div className="flex-1 flex flex-col items-center justify-center text-center p-8 text-text-tertiary">
            <div className="w-12 h-12 rounded-xl bg-surface-raised border border-border flex items-center justify-center mb-3">
              <FileCode2 className="w-6 h-6 text-text-secondary" />
            </div>
            <h3 className="text-title-14 text-text-primary mb-1">
              {selectedProfileId ? "Query Console Ready" : "No Connection Selected"}
            </h3>
            <p className="text-body-13 max-w-sm text-text-secondary">
              {selectedProfileId
                ? `Type a SQL query above and click Run (or press ${shortcut("Enter")}). Autocomplete will suggest schemas and tables as you type.`
                : "Select a connection profile in the sidebar to start writing queries."}
            </p>
          </div>
        )}
      </div>
      </ResizablePanel>
      </ResizablePanelGroup>

      {/* Plan Viewer Modal (Screen 22) */}
      {activePlan && (
        <QueryPlanView plan={activePlan} onClose={() => setActivePlan(null)} />
      )}
    </div>
  );
}
