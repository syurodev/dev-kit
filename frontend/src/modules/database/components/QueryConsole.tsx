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
  KeyRound,
  Loader2,
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
import { cn } from "@/lib/utils";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { requestVaultUnlock } from "@/workbench/vaultUnlockRequest";
import { isVaultLockedError } from "../errorStates";
import { useShallow } from "zustand/react/shallow";
import { EMPTY_CONSOLE, useDatabaseStore } from "../databaseStore";
import { getEngineMeta } from "./EngineBadge";
import { explain, explainAnalyze, type QueryPlan } from "../../../bridge/database";
import { QueryPlanView } from "./QueryPlanView";
import { ResultGrid } from "../grid/ResultGrid";
import { resizeColumn } from "../grid/gridColumns";

/**
 * Figma `DB / Console Content` (742:8). A toolbar row with Run ⌘↵ and Export
 * on the right; the editor and the result each a well-surfaced card inset 24
 * from the content edge; between them the row count and the wall-clock time
 * in Body/13 Medium, with a cap chip on the right when the result was cut.
 * The table is Code/12 throughout — header on surface/raised in Medium, the
 * first column primary, the rest secondary, NULL in the warning colour.
 *
 * Kept though the page does not draw them: Explain, Explain Analyze, Save, the
 * resizable split and the banners.
 */
export function QueryConsole({ consoleId }: { consoleId: string }) {
  // One subscription per console: the slice is a single object, so a run in
  // another console changes nothing this component reads.
  // Not named `console`: shadowing the global inside a 600-line component is a
  // trap for the next person who reaches for console.error in here.
  const slice = useDatabaseStore((s) => s.consoles[consoleId] ?? EMPTY_CONSOLE);
  const setSql = useDatabaseStore((s) => s.setSql);
  const executeQuery = useDatabaseStore((s) => s.executeQuery);
  const cancelQuery = useDatabaseStore((s) => s.cancelQuery);
  const setConsoleEditorSize = useDatabaseStore((s) => s.setConsoleEditorSize);
  const setQueryError = useDatabaseStore((s) => s.setQueryError);
  const {
    profiles,
    selectedProfileId,
    clearQueryError,
    schemaSnapshot,
    setInspectedCell,
    openExportModal,
    openSaveQueryModal,
  } = useDatabaseStore(
    useShallow((s) => ({
      profiles: s.profiles,
      selectedProfileId: s.selectedProfileId,
      clearQueryError: s.clearQueryError,
      schemaSnapshot: s.schemaSnapshot,
      setInspectedCell: s.setInspectedCell,
      openExportModal: s.openExportModal,
      openSaveQueryModal: s.openSaveQueryModal,
    }))
  );

  // queryResult and queryStartTime are read inside nested closures below
  // (map callbacks, a setInterval tick). TypeScript narrows a plain const
  // across a closure boundary but not a property access on a mutable object,
  // so a null-check on `slice.queryResult` wouldn't survive into a `.map`
  // callback — these locals give the narrowing something it can keep.
  const { queryResult, queryStartTime } = slice;

  // Console widths are per mount, not stored: a console runs a different query
  // every time, and a width saved against the last result describes columns
  // this one may not have.
  const [gridWidths, setGridWidths] = useState<number[]>(EMPTY_ARRAY);

  const [elapsed, setElapsed] = useState(0);
  const [activePlan, setActivePlan] = useState<QueryPlan | null>(null);
  const [explaining, setExplaining] = useState(false);

  const profile = profiles.find((p) => p.id === selectedProfileId);
  const engineMeta = getEngineMeta(profile?.engine);

  // 30s Execution timer (Screen 14)
  useEffect(() => {
    if (!slice.runningQuery || !queryStartTime) {
      setElapsed(0);
      return;
    }
    const interval = setInterval(() => {
      const now = Date.now();
      const diffSec = (now - queryStartTime) / 1000;
      setElapsed(diffSec);
    }, 100);
    return () => clearInterval(interval);
  }, [slice.runningQuery, queryStartTime]);

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
      return /\$\d+/.test(slice.sql);
    }
    return false;
  }, [profile?.engine, slice.sql]);

  // Check if error is safe-mode / read-only rejection (Screen 18)
  const isReadOnlyRejection =
    slice.queryError.toLowerCase().includes("read-only") ||
    slice.queryError.includes("25006") ||
    slice.queryError.toLowerCase().includes("transaction is read-only");

  async function handleExplain(analyze: boolean) {
    if (!selectedProfileId || !slice.sql.trim()) return;
    setExplaining(true);
    clearQueryError(consoleId);
    try {
      const plan = analyze
        ? await explainAnalyze(selectedProfileId, slice.sql)
        : await explain(selectedProfileId, slice.sql);
      setActivePlan(plan);
    } catch (e: any) {
      setQueryError(consoleId, e.message || String(e));
    } finally {
      setExplaining(false);
    }
  }

  // Keyboard shortcut Ctrl+Enter / Cmd+Enter to run query
  function handleKeyDown(e: React.KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      void executeQuery(consoleId);
    }
  }

  const busy = slice.runningQuery || explaining || !selectedProfileId;

  return (
    <div className="flex h-full flex-1 flex-col overflow-hidden" onKeyDown={handleKeyDown}>
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
          onClick={() => openSaveQueryModal(consoleId)}
          disabled={!slice.sql.trim()}
        >
          <BookmarkPlus className="size-3.5" />
          Save
        </Button>

        <div className="flex-1" />

        {queryResult && (
          <Button
            variant="outline"
            size="default"
            className="bg-surface-raised"
            onClick={() => openExportModal(queryResult)}
          >
            <Download className="size-3.5" />
            Export
          </Button>
        )}
        {/* The wait belongs in the control that started it (smoothness 5), and
            Cancel appears beside it rather than replacing it — the Run button
            keeps its place in the row so nothing else shifts. */}
        {slice.runningQuery && (
          <Button
            variant="outline"
            size="default"
            className="bg-surface-raised"
            onClick={() => cancelQuery(consoleId)}
          >
            <XCircle className="size-3.5" />
            Cancel
          </Button>
        )}
        <Button
          variant="default"
          size="default"
          onClick={() => void executeQuery(consoleId)}
          disabled={slice.runningQuery || !selectedProfileId}
        >
          {slice.runningQuery ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <Play className="size-3.5 fill-current" />
          )}
          {slice.runningQuery ? "Running…" : "Run"}
          <span className="text-code-11 opacity-65">{shortcut("Enter")}</span>
        </Button>
      </div>

      {/* The editor used to be a hard 220px. A long query and a wide result
          set want different splits, so the split is dragged, remembered, and
          keyboard-resizable — the handle is a real separator, not a bare div. */}
      <ResizablePanelGroup
        orientation="vertical"
        className="min-h-0 flex-1"
        defaultLayout={{ editor: slice.editorSize }}
        onLayoutChanged={(layout, meta) => {
          // Only a real drag is worth writing through ui_state — the callback
          // also fires on mount and on every constraint recompute.
          if (meta.isUserInteraction) setConsoleEditorSize(consoleId, layout.editor);
        }}
      >
        {/* Sizes are strings on purpose: this library reads a bare number as
            pixels, so minSize={15} would be fifteen pixels, not fifteen
            percent. */}
        <ResizablePanel id="editor" minSize="15" className="flex flex-col">
          {/* editor (742:23): a well with a subtle stroke, radius 8. */}
          <div className="relative flex-1 overflow-hidden rounded-md border border-border-subtle bg-surface-well">
            <CodeEditor
              value={slice.sql}
              onChange={(value) => setSql(consoleId, value)}
              extensions={sqlExtension}
              wrap
            />
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
                onClick={() => setSql(consoleId, slice.sql.replace(/\$\d+/g, "?"))}
              >
                Auto-fix placeholders
              </Button>
            </div>
          )}

          {/* 30s Execution Timer Bar (Screen 14) */}
          {slice.runningQuery && (
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
                  {slice.queryError}
                </div>
              </div>
              <button
                type="button"
                className="p-1 text-text-tertiary hover:text-text-primary"
                onClick={() => clearQueryError(consoleId)}
              >
                <XCircle className="size-4" />
              </button>
            </div>
          )}

          {/* Vault locked mid-session (Screen 26). Deliberately a banner, not a
              replacement: the rows on screen came out of the database before the
              vault shut and are already in memory. Clearing them would destroy
              work the lock has no claim on. */}
          {isVaultLockedError(slice.queryError) && (
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
          {slice.queryError && !isReadOnlyRejection && !isVaultLockedError(slice.queryError) && (
            <div className="mb-2 flex shrink-0 items-start gap-2.5 rounded-md border border-(--border-danger) bg-(--feedback-danger-soft) p-3 text-body-12 text-status-danger">
              <AlertCircle className="mt-0.5 size-4 shrink-0" />
              <div className="flex-1 space-y-1">
                <div className="text-body-13-medium">Query execution error</div>
                <div className="rounded border border-border bg-surface-base p-2 text-code-11 whitespace-pre-wrap text-text-primary">
                  {slice.queryError}
                </div>
              </div>
              <button
                type="button"
                className="p-1 text-text-tertiary hover:text-text-primary"
                onClick={() => clearQueryError(consoleId)}
              >
                <XCircle className="size-4" />
              </button>
            </div>
          )}

          {queryResult ? (
            /* The previous result stays put while the next statement runs and
               is only dimmed (smoothness 5, "no clearing the old result"). It
               is also inert: clicking a cell of a table that is about to be
               replaced opens an inspector on stale data. */
            <div
              className={cn(
                "flex min-h-0 flex-1 flex-col",
                slice.runningQuery && "pointer-events-none opacity-60 transition-opacity duration-(--motion-base)",
              )}
            >
              {/* 742:36–742:39: "200 rows  412 ms" and the cap chip. */}
              <div className="flex h-9 shrink-0 items-center gap-6">
                <span className="text-body-13-medium text-text-primary">
                  <span className="tabular-nums">{queryResult.rowCount}</span>{" "}
                  {queryResult.rowCount === 1 ? "row" : "rows"}
                </span>
                {slice.queryDurationMs != null && (
                  <span className="text-body-13-medium text-text-primary tabular-nums">{slice.queryDurationMs} ms</span>
                )}
                <span className="text-caption-11 text-text-tertiary">
                  <span className="tabular-nums">{queryResult.columns.length}</span> columns
                </span>
                <span className="flex-1" />
                {queryResult.truncated && (
                  <span className="flex h-7 items-center rounded-md border border-border-subtle bg-surface-raised px-3 text-code-11 text-text-tertiary">
                    row cap reached — add LIMIT/OFFSET to page further
                  </span>
                )}
              </div>

              {/* result (743:8): a well card; header on surface/raised. */}
              <ResultGrid
                result={queryResult}
                widths={gridWidths}
                columnOrder={[]}
                onResize={(index, delta) => setGridWidths((w) => resizeColumn(w, index, delta))}
                onInspectCell={setInspectedCell}
              />
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
