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
} from "lucide-react";
import { sql as codemirrorSql, PostgreSQL, MySQL, MariaSQL } from "@codemirror/lang-sql";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { useDatabaseStore } from "../databaseStore";
import { EngineBadge, getEngineMeta } from "./EngineBadge";
import { explain, explainAnalyze, type QueryPlan } from "../../../bridge/database";
import { QueryPlanView } from "./QueryPlanView";

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
    <div className="flex-1 flex flex-col h-full overflow-hidden bg-surface-base" onKeyDown={handleKeyDown}>
      {/* Editor Section */}
      <div className="h-[220px] shrink-0 flex flex-col border-b border-border bg-surface-base">
        {/* Editor Toolbar */}
        <div className="flex flex-wrap items-center justify-between px-3 py-1.5 border-b border-border/60 bg-surface-raised/40 gap-2">
          <div className="flex items-center gap-2">
            <Button
              variant="default"
              size="sm"
              className="h-7 text-xs gap-1.5 font-medium shadow-xs"
              onClick={() => void executeQuery()}
              disabled={runningQuery || !selectedProfileId}
            >
              <Play className="w-3.5 h-3.5 fill-current" />
              <span>Run</span>
              <kbd className="hidden sm:inline-block text-[10px] opacity-70 ml-1">⌘↵</kbd>
            </Button>

            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs gap-1 text-text-secondary"
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
              className="h-7 text-xs gap-1 text-text-secondary"
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
              className="h-7 text-xs gap-1 text-text-secondary hover:text-text-primary"
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
                className="h-7 text-xs gap-1 text-text-secondary"
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
      </div>

      {/* DevKit Advice for MySQL placeholder mismatch (Screen 21) */}
      {hasPlaceholderMismatch && (
        <div className="bg-amber-500/10 border-b border-amber-500/30 px-4 py-2 flex items-center justify-between text-xs text-amber-500 shrink-0">
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
            className="h-6 text-[11px] text-amber-500 hover:bg-amber-500/20"
            onClick={() => setSql(sql.replace(/\$\d+/g, "?"))}
          >
            Auto-fix placeholders
          </Button>
        </div>
      )}

      {/* 30s Execution Timer Bar (Screen 14) */}
      {runningQuery && (
        <div className="bg-surface-raised border-b border-border px-4 py-2.5 flex items-center justify-between text-xs shrink-0">
          <div className="flex items-center gap-3 flex-1 max-w-md">
            <div className="flex items-center gap-1.5 text-text-primary font-medium">
              <Clock className="w-4 h-4 text-action-primary animate-pulse" />
              <span>Executing statement…</span>
              <span className="font-mono text-action-primary">{elapsed.toFixed(1)}s</span>
            </div>
            <div className="flex-1 h-1.5 bg-surface-base rounded-full overflow-hidden border border-border">
              <div
                className="h-full bg-action-primary transition-all duration-100 ease-out"
                style={{ width: `${Math.min(100, (elapsed / 30) * 100)}%` }}
              />
            </div>
            <span className="text-[11px] text-text-tertiary">30s cap</span>
          </div>
        </div>
      )}

      {/* Safe-Mode Rejected Banner (Screen 18) */}
      {isReadOnlyRejection && (
        <div className="bg-status-danger/10 border-b border-status-danger/30 p-3 flex items-start gap-2.5 text-xs text-status-danger shrink-0">
          <ShieldAlert className="w-4 h-4 shrink-0 mt-0.5" />
          <div className="flex-1 space-y-1">
            <div className="font-semibold flex items-center gap-2">
              <span>Operation Rejected by Database Engine (Safe-Mode Policy)</span>
              <span className="font-mono text-[10px] bg-status-danger/20 px-1 py-0.5 rounded">
                SQLSTATE 25006
              </span>
            </div>
            <p className="text-text-secondary text-[11px] leading-relaxed">
              This connection is operating in <strong>Safe-Mode (Read-Only)</strong>. Mutating queries (INSERT, UPDATE, DELETE, DDL) are blocked by the database session. You can change this setting in Connection Options.
            </p>
            <div className="font-mono text-[11px] text-text-tertiary bg-surface-base p-1.5 rounded border border-border mt-1">
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

      {/* Generic Error Banner (Screen 13) */}
      {queryError && !isReadOnlyRejection && (
        <div className="bg-status-danger/10 border-b border-status-danger/30 p-3 flex items-start gap-2.5 text-xs text-status-danger shrink-0">
          <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
          <div className="flex-1 space-y-1">
            <div className="font-semibold">Query Execution Error</div>
            <div className="font-mono text-[11px] text-text-primary bg-surface-base p-2 rounded border border-border whitespace-pre-wrap">
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
        <div className="bg-status-success/10 border-b border-status-success/20 px-4 py-1.5 flex items-center justify-between text-xs text-status-success shrink-0">
          <span className="flex items-center gap-1.5 font-medium">
            <CheckCircle2 className="w-3.5 h-3.5" />
            {queryStatus}
          </span>
          <span className="text-[11px] text-text-tertiary">
            Double-click any cell to inspect its content
          </span>
        </div>
      )}

      {/* Main Content Area: Result Table or Empty state */}
      <div className="flex-1 overflow-auto bg-surface-base flex flex-col">
        {queryResult ? (
          <div className="flex flex-col h-full">
            {/* Table Meta Bar */}
            <div className="px-4 py-2 border-b border-border flex items-center justify-between text-xs bg-surface-raised/30 shrink-0">
              <div className="flex items-center gap-2 font-medium text-text-primary">
                <TableIcon className="w-3.5 h-3.5 text-text-secondary" />
                <span>{queryResult.rowCount} row(s)</span>
                {queryResult.truncated && (
                  <Badge variant="secondary" className="text-[10px] bg-amber-500/15 text-amber-500 border-amber-500/30">
                    Capped at 200 rows
                  </Badge>
                )}
              </div>
              <div className="flex items-center gap-2 text-text-tertiary text-[11px]">
                <span>{queryResult.columns.length} columns</span>
              </div>
            </div>

            {/* Virtualized/Scrollable Table */}
            <div className="flex-1 overflow-auto">
              <table className="w-full text-left text-xs border-collapse font-mono" aria-label="Query results">
                <thead className="bg-surface-raised sticky top-0 z-10 border-b border-border shadow-2xs">
                  <tr>
                    <th className="w-12 px-3 py-2 text-center text-text-tertiary font-semibold border-r border-border/40 select-none bg-surface-raised">
                      #
                    </th>
                    {queryResult.columns.map((col) => (
                      <th
                        key={col}
                        className="px-3 py-2 text-text-primary font-semibold border-r border-border/40 bg-surface-raised truncate max-w-[240px]"
                      >
                        {col}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-border/40 text-text-primary">
                  {queryResult.rows.map((row, i) => (
                    <tr
                      key={i}
                      className="hover:bg-surface-raised/60 transition-colors group"
                    >
                      <td className="w-12 px-3 py-2 text-center text-text-tertiary text-[11px] border-r border-border/40 select-none bg-surface-base group-hover:bg-surface-raised/40">
                        {i + 1}
                      </td>
                      {row.map((cell, j) => {
                        const colName = queryResult.columns[j] || `col_${j}`;
                        return (
                          <td
                            key={j}
                            className="px-3 py-2 text-text-secondary border-r border-border/40 truncate max-w-[280px] cursor-pointer hover:text-text-primary hover:bg-action-primary/10 select-text"
                            onClick={() => setInspectedCell({ columnName: colName, value: cell })}
                            title="Click to inspect cell"
                          >
                            {cell === "" ? (
                              <span className="text-text-tertiary italic text-[10px]">NULL</span>
                            ) : (
                              cell
                            )}
                          </td>
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
            <h3 className="text-sm font-semibold text-text-primary mb-1">
              {selectedProfileId ? "Query Console Ready" : "No Connection Selected"}
            </h3>
            <p className="text-xs max-w-sm text-text-secondary leading-relaxed">
              {selectedProfileId
                ? "Type a SQL query above and click Run (or press Cmd+Enter). Autocomplete will suggest schemas and tables as you type."
                : "Select a connection profile in the sidebar to start writing queries."}
            </p>
          </div>
        )}
      </div>

      {/* Plan Viewer Modal (Screen 22) */}
      {activePlan && (
        <QueryPlanView plan={activePlan} onClose={() => setActivePlan(null)} />
      )}
    </div>
  );
}
