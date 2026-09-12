import { useMemo, useRef, useState } from "react";
import {
  FileCode2,
  BookmarkPlus,
  Download,
  ShieldAlert,
  Info,
  XCircle,
  Sliders,
  KeyRound,
} from "lucide-react";
import { sql as codemirrorSql, PostgreSQL, MySQL, MariaSQL } from "@codemirror/lang-sql";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { shortcut } from "@/lib/shortcut";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { Prec } from "@codemirror/state";
import { keymap } from "@codemirror/view";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";
import { cn } from "@/lib/utils";
import { consoleStatus } from "../consoleStatus";
import { RunGroup } from "./RunGroup";
import { ConsoleBinding } from "./ConsoleBinding";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { requestVaultUnlock } from "@/workbench/vaultUnlockRequest";
import { isVaultLockedError } from "../errorStates";
import { useShallow } from "zustand/react/shallow";
import { EMPTY_CONSOLE, useDatabaseStore } from "../databaseStore";
import { getEngineMeta } from "./EngineBadge";
import { explain, explainAnalyze, type QueryPlan } from "../../../bridge/database";
import { QueryPlanView } from "./QueryPlanView";
import { sqlCompletionTheme } from "./sqlCompletionTheme";
import { ResultGrid } from "../grid/ResultGrid";

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
    selectedSchema,
    clearQueryError,
    schemaSnapshot,
    setInspectedCell,
    openExportModal,
    openSaveQueryModal,
  } = useDatabaseStore(
    useShallow((s) => ({
      profiles: s.profiles,
      selectedProfileId: s.selectedProfileId,
      selectedSchema: s.selectedSchema,
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

  /*
   * The connection the FILE was written for, and only when it is not the one
   * selected now. Restoring a session is where the two part company: the tabs
   * come back with their text and the module opens on whichever connection is
   * first, which need not be the one the text was written against.
   *
   * A connection that has since been deleted is named as one rather than by
   * its id — the id is an opaque record key and means nothing to the reader.
   */
  const writtenFor =
    slice.originProfileId && slice.originProfileId !== selectedProfileId
      ? (profiles.find((p) => p.id === slice.originProfileId)?.name ??
        "a connection that no longer exists")
      : undefined;

  // Console widths are per mount, not stored: a console runs a different query
  // every time, and a width saved against the last result describes columns
  // this one may not have.
  const [gridWidths, setGridWidths] = useState<number[]>(EMPTY_ARRAY);

  const [activePlan, setActivePlan] = useState<QueryPlan | null>(null);
  const [explaining, setExplaining] = useState(false);

  const profile = profiles.find((p) => p.id === selectedProfileId);
  const engineMeta = getEngineMeta(profile?.engine);

  /* The 30s execution clock lives in RunGroup now. It ticked ten times a
     second HERE, and this component owns the editor and the result grid — so
     every tick re-rendered a table of rows nobody had asked to change, all the
     way through the run, which is what the split animation was competing
     with. The pill is the only thing that moves; it keeps its own interval. */

  // Read by the editor's own ⌘↵ binding, which is built once and must not be
  // rebuilt every time the callback identity changes.
  const runRef = useRef<() => void>(() => {});
  runRef.current = () => void executeQuery(consoleId);

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
      sqlCompletionTheme,
      // Highest precedence, because basicSetup's default keymap already owns
      // Mod-Enter (insertBlankLine) and whoever gets there first wins.
      // Returning true consumes the key, which is also what stops the console
      // wrapper's handler from running the statement a second time.
      Prec.highest(
        keymap.of([
          {
            key: "Mod-Enter",
            run: () => {
              runRef.current();
              return true;
            },
          },
        ]),
      ),
    ];
  }, [profile?.engine, profile?.database, schemaSnapshot?.namespace, runRef]);

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
  /**
   * ⌘↵ from anywhere in the console that is not the editor.
   *
   * The editor binds it itself, at the highest precedence, because CodeMirror
   * has to be the one to CONSUME it: basicSetup's default keymap maps
   * Mod-Enter to insertBlankLine, and that runs on the content element long
   * before a keydown reaches this ancestor — so the statement ran AND a blank
   * line was inserted. Bailing on an event the editor already handled is what
   * stops the two from both firing.
   */
  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.defaultPrevented) return;
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      void executeQuery(consoleId);
    }
  }

  const busy = slice.runningQuery || explaining || !selectedProfileId;

  // One line about the statement that just ran, for the pill beside Run. The
  // wording and the ranking live in consoleStatus so they can be tested.
  const outcome = consoleStatus({
    running: false,
    elapsedSec: 0,
    durationMs: slice.queryDurationMs ?? undefined,
    error: slice.queryError || undefined,
    rowCount: queryResult?.rowCount,
    sql: slice.sql,
  });

  return (
    <div className="flex h-full flex-1 flex-col overflow-hidden" onKeyDown={handleKeyDown}>
      {/* Toolbar (742:18, 742:21): icon-only, the way a toolbar acting on the
          document below it is icon-only everywhere else in this module. The
          labels were costing the row 300px to say three words that the icons
          and their tooltips say — and Explain, Explain analyze and Save are
          not the point of this view, the editor under them is.

          Same controls as the table tab's strip, because it is the same kind
          of row: ghost at icon-sm, no fill of its own, 2px apart. Outline
          buttons on a raised fill made the console's row read as a different
          component from the table's — two toolbars in one module, doing the
          same job, telling you they were unrelated.

          Run is the one exception and keeps the filled primary: it is the
          point of this view, and the table's strip has nothing like it. Its
          shortcut moves into the tooltip, because a badge inside an icon-only
          button is not an icon-only button.

          Every one of them is wrapped: an icon with no accessible name is a
          button a screen reader can only call "button", and ActionTooltip is
          what sets the name as well as the tip. The two that can be disabled
          take `wrap`, because a disabled button raises no tooltip on its
          own. */}
      <div className="mb-2 flex h-(--control-height-md) shrink-0 items-center gap-0.5">
        <ActionTooltip label="Explain the query plan without executing">
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => void handleExplain(false)}
            disabled={busy}
          >
            <FileCode2 />
          </Button>
        </ActionTooltip>
        <ActionTooltip label="Explain analyze — run it in a rolled-back transaction to measure the real time">
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => void handleExplain(true)}
            disabled={busy}
          >
            <Sliders />
          </Button>
        </ActionTooltip>
        <ActionTooltip label="Save this query" wrap>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => openSaveQueryModal(consoleId)}
            disabled={!slice.sql.trim()}
          >
            <BookmarkPlus />
          </Button>
        </ActionTooltip>

        {/* Which connection this console's statements go to. In the toolbar
            rather than a status bar, because the button it describes is at
            the other end of this same row. */}
        <ConsoleBinding
          connection={profile?.name ?? ""}
          database={selectedSchema || profile?.database || ""}
          engine={profile?.engine}
          writtenFor={writtenFor}
        />

        <div className="flex-1" />

        {queryResult && (
          <ActionTooltip label="Export the result">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => openExportModal(queryResult)}
            >
              <Download />
            </Button>
          </ActionTooltip>
        )}
        {/* Run, and what came back, in one body — see RunGroup. The clock
            used to live in a bar between the editor and the result; it is in
            the pill now, which is the control that started it, and the bar
            that said the same thing in a band of its own is gone. */}
        <RunGroup
          outcome={outcome}
          running={slice.runningQuery}
          startedAt={queryStartTime}
          canRun={Boolean(selectedProfileId)}
          onRun={() => void executeQuery(consoleId)}
          onStop={() => cancelQuery(consoleId)}
          runLabel={
            slice.runningQuery
              ? "Stop — cancel the running query"
              : `Run the query — ${shortcut("Enter")}`
          }
        />
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
          {/* No generic error banner. The pill beside Run carries the first
              line, and one click opens the rest over the editor with a copy
              button — which is the same information without a block that
              pushed the query you just wrote further down the screen every
              time it failed. The two banners above stay: each explains a
              policy and names the way out, which is more than an error text
              has to give. */}

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
                onResize={setGridWidths}
                onInspectCell={setInspectedCell}
              />
            </div>
          ) : (
            <div className="flex flex-1 flex-col rounded-md border border-border-subtle bg-surface-well">
              <EmptyState
                icon={FileCode2}
                title={selectedProfileId ? "Query console ready" : "No connection selected"}
                body={
                  selectedProfileId
                    ? `Type a SQL query above and press Run (or ${shortcut("Enter")}). Autocomplete suggests schemas and tables as you type.`
                    : "Select a connection profile in the sidebar to start writing queries."
                }
              />
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
