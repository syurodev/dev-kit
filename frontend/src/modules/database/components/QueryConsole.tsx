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
  X,
} from "lucide-react";
import { sql as codemirrorSql, PostgreSQL, MySQL, MariaSQL } from "@codemirror/lang-sql";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import { shortcut } from "@/lib/shortcut";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { Prec } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";
import { cn } from "@/lib/utils";
import { consoleStatus } from "../consoleStatus";
import { dialectOf, splitStatements, statementAt, statementName } from "../sql/statements";
import { statementStatusGutter } from "../sql/statementGutter";
import { RunGroup } from "./RunGroup";
import { ConsoleBinding } from "./ConsoleBinding";
import { Tabs, TabsCount, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { requestVaultUnlock } from "@/workbench/vaultUnlockRequest";
import { isVaultLockedError } from "../errorStates";
import { useShallow } from "zustand/react/shallow";
import { EMPTY_CONSOLE, useDatabaseStore, visibleResult } from "../databaseStore";
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
    clearQueryResult,
    runScript,
    setActiveResult,
    closeResult,
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
      clearQueryResult: s.clearQueryResult,
      runScript: s.runScript,
      setActiveResult: s.setActiveResult,
      closeResult: s.closeResult,
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
  const { queryStartTime } = slice;
  const queryResult = visibleResult(slice);

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
  // Separate from the plan: closing keeps the plan mounted so the dialog can
  // animate out with its content still in it, instead of vanishing.
  const [planOpen, setPlanOpen] = useState(false);
  const [explaining, setExplaining] = useState(false);

  const profile = profiles.find((p) => p.id === selectedProfileId);
  const engineMeta = getEngineMeta(profile?.engine);

  /* The 30s execution clock lives in RunGroup now. It ticked ten times a
     second HERE, and this component owns the editor and the result grid — so
     every tick re-rendered a table of rows nobody had asked to change, all the
     way through the run, which is what the split animation was competing
     with. The pill is the only thing that moves; it keeps its own interval. */

  /*
   * Where the caret is, without a render per keystroke.
   *
   * Run means "run the statement the caret is in" — DataGrip's rule, and the
   * only one that makes a console with several statements work at all: the
   * whole text is not something either driver will accept, pgx because it
   * prepares, MySQL because multi-statement is a DSN flag nobody should set.
   *
   * A ref rather than state because nothing on screen depends on the caret
   * moving; the two things that read it are pressed, not rendered.
   */
  const selectionRef = useRef({ from: 0, to: 0 });
  // Named apart from the CodeMirror SQLDialect inside the memo below, which
  // is a different thing with the same job title.
  const splitDialect = useMemo(() => dialectOf(profile?.engine), [profile?.engine]);

  const statements = useMemo(
    () => splitStatements(slice.sql, splitDialect),
    [slice.sql, splitDialect],
  );

  const caretHead = useRef(0);

  /*
   * One button, and DataGrip's rule behind it: what is selected, or the
   * whole console.
   *
   * The second button is gone. A console with one statement — which is most
   * of them — had two controls that did the same thing, and the one that
   * appeared and disappeared as you typed a semicolon was the worse of the
   * two to reason about.
   *
   * Selecting is how you run one statement out of five, and it is a
   * gesture, not a shortcut to remember. The caret rule is still there on
   * ⇧⌘↵ for when the statement is long and selecting it is work.
   */
  const runFromEditor = () => {
    const { from, to } = selectionRef.current;
    const source = from === to ? slice.sql : slice.sql.slice(Math.min(from, to), Math.max(from, to));
    const selected = from === to ? statements : splitStatements(source, splitDialect);
    if (selected.length === 0) return;
    void runScript(
      consoleId,
      selected.map((statement) => statement.text),
    );
  };

  /** Just the one the caret is in, leaving the other marks alone. */
  const runStatement = () => {
    const statement = statementAt(statements, caretHead.current);
    if (!statement) return;
    void executeQuery(consoleId, statement.text);
  };

  // Read by the editor's own ⌘↵ binding, which is built once and must not be
  // rebuilt every time the callback identity changes.
  const runRef = useRef<() => void>(() => {});
  runRef.current = runFromEditor;
  const runStatementRef = useRef<() => void>(() => {});
  runStatementRef.current = runStatement;

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
      // A tick or a cross beside each statement that has run. Rebuilt only
      // when an outcome changes — the split inside it is memoised per
      // document, so typing costs one split per keystroke, not one per
      // visible line.
      statementStatusGutter(slice.statementRuns, splitDialect),
      // The caret, into a ref. selectionSet fires for every cursor move and
      // for none of them does anything on screen change.
      EditorView.updateListener.of((update) => {
        const { from, to, head } = update.state.selection.main;
        selectionRef.current = { from, to };
        caretHead.current = head;
      }),
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
          {
            key: "Mod-Shift-Enter",
            run: () => {
              runStatementRef.current();
              return true;
            },
          },
        ]),
      ),
    ];
  }, [profile?.engine, profile?.database, schemaSnapshot?.namespace, runRef, runStatementRef, splitDialect, slice.statementRuns]);

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

  /*
   * The statement Explain is about: the first one in the selection, or the
   * one the caret is in.
   *
   * It was the whole console. Run learned "what you selected" and Explain
   * kept sending slice.sql, so selecting line 1 of two and pressing Explain
   * sent both — and EXPLAIN takes one statement by definition, so the engine
   * choked on the second one you had deliberately not selected.
   *
   * One, not "the selection": a plan is of a statement. A selection spanning
   * three explains the first, which is the one it starts in.
   */
  const explainTarget = () => {
    const { from, to } = selectionRef.current;
    if (from !== to) {
      const selected = splitStatements(
        slice.sql.slice(Math.min(from, to), Math.max(from, to)),
        splitDialect,
      );
      return selected[0]?.text;
    }
    return statementAt(statements, caretHead.current)?.text;
  };

  async function handleExplain(analyze: boolean) {
    const target = explainTarget();
    if (!selectedProfileId || !target) return;
    setExplaining(true);
    clearQueryError(consoleId);
    try {
      const plan = analyze
        ? await explainAnalyze(selectedProfileId, target)
        : await explain(selectedProfileId, target);
      setActivePlan(plan);
      setPlanOpen(true);
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
      // The same rule the editor's own binding and the Run button use. This
      // path still ran the whole text regardless of the selection, so ⌘↵
      // meant two different things depending on where focus happened to be.
      runFromEditor();
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
    // What RAN, not what is in the console: a SELECT run from a script that
    // also holds an UPDATE must not be warned about the UPDATE.
    sql: slice.lastStatement || slice.sql,
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
          onRun={runFromEditor}
          onStop={() => cancelQuery(consoleId)}
          runLabel={
            slice.runningQuery
              ? "Stop — cancel the running query"
              : statements.length > 1
                ? `Run all ${statements.length} statements, or just what you select — ${shortcut("Enter")}`
                : `Run the query — ${shortcut("Enter")}`
          }
        />
      </div>

      {/* The notices, above the split rather than inside the result panel.
          Two of the three explain why a statement produced NOTHING — a
          rejected write, a placeholder the dialect will not take — and the
          result panel is gone in exactly that case now. A banner that only
          renders when there is a result cannot explain the absence of one. */}
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

        {/*
          The result is a slot, and a slot with nothing in it is not drawn.

          It used to be an empty card telling you to type a query, under an
          editor you were already typing in — a permanent third of the
          console spent on instructions nobody needed twice. Gone, and the
          handle with it, so an unrun console is all editor.
        */}
        {queryResult ? (
          <>
            {/* The window gutter's own 8 — the band the shell leaves between
                its surfaces and the window edge — so the editor and the
                result read as two cards on one rhythm.

                The separator itself is collapsed to nothing and its hairline
                dropped: at 8px apart, with a border on each card, a line
                between them is a third edge in a 9px band. What is left is
                the grip and the 4px hit area, both of which are drawn
                outside the box. */}
            <ResizableHandle
              withHandle
              className="my-1 bg-transparent aria-[orientation=horizontal]:h-0"
            />

            <ResizablePanel
              id="results"
              minSize="20"
              className="flex min-h-0 flex-col overflow-hidden"
            >
              {/* The previous result stays put while the next statement runs
                  and is only dimmed (smoothness 5, "no clearing the old
                  result"). It is also inert: clicking a cell of a table that
                  is about to be replaced opens an inspector on stale data. */}
              <div
                className={cn(
                  "flex min-h-0 flex-1 flex-col",
                  slice.runningQuery &&
                    "pointer-events-none opacity-60 transition-opacity duration-(--motion-base)",
                )}
              >
                {/*
                  What the header says now: the cap, and the way out.

                  "26 rows", the duration and the column count all left. Every
                  one of them is in the pill beside Run, six inches up and to
                  the right, and printing a number twice makes the reader
                  check whether the two agree — which is how the "0.0s" next
                  to "32 ms" was found.

                  The cap chip stays because nothing else says it: a result
                  cut at the row ceiling looks exactly like a complete one.
                */}
                <div className="mb-2 flex h-8 shrink-0 items-center gap-2">
                  {/*
                    One tab per grid the run produced, including when there
                    is one of them. A script's SELECTs each have an answer;
                    without these the reader watches three go past and keeps
                    the last. And the single tab is not noise — it names the
                    statement the rows came from, which in a console holding
                    five of them is the question the grid raises.

                    Named after the table the statement touches, because with
                    four tabs open "ba_account" is what the eye is looking
                    for — every one of them starts "SELECT * FROM".
                  */}
                  {slice.results.length > 0 && (
                    <Tabs
                      value={String(slice.activeResult)}
                      onValueChange={(value) => setActiveResult(consoleId, Number(value))}
                      className="min-w-0"
                    >
                      {/* The well the editor above it sits in, not the
                          strip's own default track. That default is black at
                          25% — right on the rail, where it is an inset in a
                          transparent surface, and a foreign dark band here on
                          a card where every other recess is --surface-well. */}
                      <TabsList className="max-w-full overflow-x-auto bg-surface-well">
                        {slice.results.map((tab, index) => (
                          <TabsTrigger
                            key={`${index}-${tab.statement}`}
                            value={String(index)}
                            title={tab.statement}
                          >
                            {statementName(tab.statement)}
                            <TabsCount>{tab.result.rowCount}</TabsCount>
                            {/* A span, not a button: this sits inside the
                                trigger's own button and nesting one in the
                                other is markup no browser agrees on. Named
                                and keyboard-reachable all the same. */}
                            <span
                              role="button"
                              tabIndex={0}
                              aria-label={`Close ${statementName(tab.statement)}`}
                              onClick={(event) => {
                                event.stopPropagation();
                                closeResult(consoleId, index);
                              }}
                              onKeyDown={(event) => {
                                if (event.key === "Enter" || event.key === " ") {
                                  event.preventDefault();
                                  event.stopPropagation();
                                  closeResult(consoleId, index);
                                }
                              }}
                              className="flex size-3.5 shrink-0 items-center justify-center rounded-sm text-text-tertiary transition-colors hover:bg-state-soft hover:text-text-primary focus-visible:ring-1 focus-visible:ring-status-info focus-visible:outline-none"
                            >
                              <X className="size-3" />
                            </span>
                          </TabsTrigger>
                        ))}
                      </TabsList>
                    </Tabs>
                  )}
                  {queryResult.truncated && (
                    <span className="flex h-7 items-center rounded-md border border-border-subtle bg-surface-raised px-3 text-code-11 text-text-tertiary">
                      row cap reached — add LIMIT/OFFSET to page further
                    </span>
                  )}
                  <span className="flex-1" />
                  <ActionTooltip label="Close the result">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => clearQueryResult(consoleId)}
                    >
                      <X />
                    </Button>
                  </ActionTooltip>
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
            </ResizablePanel>
          </>
        ) : null}
      </ResizablePanelGroup>

      {/* Plan Viewer Modal (Screen 22) */}
      {activePlan && (
        <QueryPlanView plan={activePlan} open={planOpen} onClose={() => setPlanOpen(false)} />
      )}
    </div>
  );
}
