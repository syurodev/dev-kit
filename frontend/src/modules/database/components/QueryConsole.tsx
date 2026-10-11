import { useQuery } from "@tanstack/react-query";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  Gauge,
  BookmarkPlus,
  ShieldAlert,
  Info,
  XCircle,
  Timer,
  KeyRound,
  X,
  Play,
  Square,
  ChevronDown,
  History,
  MoreHorizontal,
} from "lucide-react";
import {
  sql as codemirrorSql,
  PostgreSQL,
  MySQL,
  MariaSQL,
} from "@codemirror/lang-sql";
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
import { dialectOf, splitStatements, statementAt } from "../sql/statements";
import { statementStatusGutter } from "../sql/statementGutter";
import { ConsoleRunStatus } from "./ConsoleRunStatus";
import { ConsoleTargetBar } from "./ConsoleTargetBar";
import { ConsoleTransactionControls } from "./ConsoleTransactionControls";
import { openHistoryTab } from "../documents";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

import { Tabs, TabsCount, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { requestVaultUnlock } from "@/workbench/vaultUnlockRequest";
import { isVaultLockedError } from "../errorStates";
import { useShallow } from "zustand/react/shallow";
import {
  EMPTY_CONSOLE,
  useDatabaseStore,
  visibleResult,
} from "../databaseStore";
import { consoleDocument, useDocumentCommands } from "../documentCommands";
import { getEngineMeta } from "./EngineBadge";
import {
  explain,
  explainAnalyze,
  getSchemaCache,
  transactionState,
  type QueryPlan,
} from "../../../bridge/database";
import { QueryPlanView } from "./QueryPlanView";
import { sqlCompletionTheme } from "./sqlCompletionTheme";
import { ResultGrid } from "../grid/ResultGrid";

/** A console owns its target, editor, layout and output; the shell owns document tabs. */
export function QueryConsole({
  consoleId,
  readTransactionState,
}: {
  consoleId: string;
  readTransactionState?: typeof transactionState;
}) {
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
    explorerProfileId,
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
      explorerProfileId: s.selectedProfileId,
      clearQueryError: s.clearQueryError,
      clearQueryResult: s.clearQueryResult,
      runScript: s.runScript,
      setActiveResult: s.setActiveResult,
      closeResult: s.closeResult,
      schemaSnapshot: s.schemaSnapshot,
      setInspectedCell: s.setInspectedCell,
      openExportModal: s.openExportModal,
      openSaveQueryModal: s.openSaveQueryModal,
    })),
  );

  // queryResult and queryStartTime are read inside nested closures below
  // (map callbacks, a setInterval tick). TypeScript narrows a plain const
  // across a closure boundary but not a property access on a mutable object,
  // so a null-check on `slice.queryResult` wouldn't survive into a `.map`
  // callback — these locals give the narrowing something it can keep.
  const { queryStartTime } = slice;
  const queryResult = visibleResult(slice);

  const selectedProfileId = slice.targetProfileId ?? explorerProfileId;
  const layoutMode = useDatabaseStore(
    (s) => s.consoleViewModes[consoleId] ?? "split",
  );
  const editorSize = useDatabaseStore(
    (s) => s.consoleEditorSizes[consoleId] ?? slice.editorSize,
  );
  const setLayoutMode = useDatabaseStore((s) => s.setConsoleViewMode);
  const [messagesOpen, setMessagesOpen] = useState(false);
  const [transactionBusy, setTransactionBusy] = useState(false);
  const hasOutput = Boolean(
    queryResult || slice.queryError || slice.queryStatus || slice.runningQuery,
  );
  const showEditor = layoutMode !== "results" || !hasOutput;
  const showResults = hasOutput && layoutMode !== "editor";
  useEffect(() => {
    setMessagesOpen(Boolean(slice.queryError));
  }, [slice.queryError, slice.queryRunId]);
  // Console widths are per mount, not stored: a console runs a different query
  // every time, and a width saved against the last result describes columns
  // this one may not have.
  const [gridWidths, setGridWidths] = useState<number[]>(EMPTY_ARRAY);

  const visible = useModuleVisibility();
  const explainRequest = useRef(0);
  useEffect(
    () => () => {
      explainRequest.current++;
    },
    [],
  );

  const [activePlan, setActivePlan] = useState<QueryPlan | null>(null);
  // Separate from the plan: closing keeps the plan mounted so the dialog can
  // animate out with its content still in it, instead of vanishing.
  const [planOpen, setPlanOpen] = useState(false);
  const [explaining, setExplaining] = useState(false);
  useEffect(() => {
    setPlanOpen(false);
    setActivePlan(null);
  }, [selectedProfileId, slice.queryRunId]);

  const profile = profiles.find((p) => p.id === selectedProfileId);
  const targetSchema = useQuery({
    queryKey: ["database", "console-schema", selectedProfileId],
    queryFn: () => getSchemaCache(selectedProfileId),
    enabled: Boolean(profile),
    retry: false,
  });
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
  const splitDialect = useMemo(
    () => dialectOf(profile?.engine),
    [profile?.engine],
  );

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
    if (explaining || transactionBusy || slice.runningQuery) return;
    const { from, to } = selectionRef.current;
    const source =
      from === to
        ? slice.sql
        : slice.sql.slice(Math.min(from, to), Math.max(from, to));
    const selected =
      from === to ? statements : splitStatements(source, splitDialect);
    if (selected.length === 0) return;
    void runScript(
      consoleId,
      selected.map((statement) => statement.text),
    );
  };

  /** Just the one the caret is in, leaving the other marks alone. */
  const runStatement = () => {
    if (explaining || transactionBusy || slice.runningQuery) return;
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
      engine === "postgres"
        ? slice.completionSchema || "public"
        : profile?.database || "public";

    return [
      codemirrorSql({
        dialect,
        schema:
          targetSchema.data?.namespace ??
          (selectedProfileId === explorerProfileId
            ? (schemaSnapshot?.namespace ?? undefined)
            : undefined),
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
          {
            // #51: Esc cancels the running statement, as ⌘. does from the
            // menu. With nothing running it stays the editor's — closing a
            // completion list, collapsing a selection.
            key: "Escape",
            run: () => {
              const state = useDatabaseStore.getState();
              if (!state.consoleState(consoleId).runningQuery) return false;
              state.cancelQuery(consoleId);
              return true;
            },
          },
        ]),
      ),
    ];
  }, [
    profile?.engine,
    profile?.database,
    slice.completionSchema,
    selectedProfileId,
    explorerProfileId,
    schemaSnapshot?.namespace,
    targetSchema.data?.namespace,
    runRef,
    runStatementRef,
    splitDialect,
    slice.statementRuns,
    consoleId,
  ]);

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
    if (!selectedProfileId || !target || explaining || slice.runningQuery)
      return;
    const request = ++explainRequest.current;
    const runId = slice.queryRunId;
    const stillCurrent = () =>
      request === explainRequest.current &&
      (useDatabaseStore.getState().consoleState(consoleId).targetProfileId ??
        useDatabaseStore.getState().selectedProfileId) === selectedProfileId &&
      useDatabaseStore.getState().consoleState(consoleId).queryRunId === runId;
    setExplaining(true);
    clearQueryError(consoleId);
    try {
      const plan = analyze
        ? await explainAnalyze(selectedProfileId, target)
        : await explain(selectedProfileId, target);
      if (!stillCurrent()) return;
      setActivePlan(plan);
      setPlanOpen(true);
    } catch (e: any) {
      if (stillCurrent()) setQueryError(consoleId, e.message || String(e));
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

  const busy = slice.runningQuery || explaining || transactionBusy || !profile;

  // #51: what the menu bar can ask of this console. Explain needs the selection
  // and the caret, which only this component holds.
  useDocumentCommands(consoleDocument(consoleId), {
    run: runFromEditor,
    explain: () => {
      if (!busy) void handleExplain(false);
    },
    explainAnalyze: () => {
      if (!busy) void handleExplain(true);
    },
    export: () => {
      if (queryResult) openExportModal(queryResult);
    },
  });

  // One line about the statement that just ran, for the pill beside Run. The
  // wording and the ranking live in consoleStatus so they can be tested.
  const outcome = consoleStatus({
    running: false,
    elapsedSec: 0,
    durationMs:
      slice.results[slice.activeResult]?.durationMs ??
      slice.queryDurationMs ??
      undefined,
    error: slice.queryError || undefined,
    rowCount: queryResult?.rowCount,
    // What RAN, not what is in the console: a SELECT run from a script that
    // also holds an UPDATE must not be warned about the UPDATE.
    sql:
      slice.results[slice.activeResult]?.statement ||
      slice.lastStatement ||
      slice.sql,
  });

  return (
    <div
      className="flex h-full flex-1 flex-col overflow-hidden"
      onKeyDown={handleKeyDown}
    >
      <ConsoleTargetBar
        consoleId={consoleId}
        disabled={explaining || transactionBusy}
      />
      <div
        className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border-subtle px-3 py-1.5"
        aria-label="SQL actions"
      >
        <div className="flex items-center">
          <Button
            size="sm"
            className="rounded-r-none"
            variant={slice.runningQuery ? "destructive" : "default"}
            disabled={!slice.runningQuery && (busy || statements.length === 0)}
            onClick={
              slice.runningQuery ? () => cancelQuery(consoleId) : runFromEditor
            }
          >
            {slice.runningQuery ? (
              <Square className="size-3.5" />
            ) : (
              <Play className="size-3.5" />
            )}
            {slice.runningQuery ? "Stop" : "Run"}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                size="icon-sm"
                className="rounded-l-none border-l border-white/20"
                disabled={busy || !statements.length}
                aria-label="Run options"
              >
                <ChevronDown className="size-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem onSelect={runFromEditor}>
                Run selection or script · {shortcut("Enter")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={runStatement}>
                Run statement at cursor · {shortcut("Enter", { shift: true })}
              </DropdownMenuItem>
              <DropdownMenuItem
                onSelect={() =>
                  void runScript(
                    consoleId,
                    statements.map((s) => s.text),
                  )
                }
              >
                Run entire script
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <Button
          variant="ghost"
          size="sm"
          disabled={busy || !statements.length}
          onClick={() => void handleExplain(false)}
        >
          <Gauge className="size-3.5" /> Explain
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => openHistoryTab(selectedProfileId)}
        >
          <History className="size-3.5" /> History
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="More SQL actions"
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuItem
              disabled={busy || !statements.length}
              onSelect={() => void handleExplain(true)}
            >
              <Timer /> Explain analyze (executes query)
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={!slice.sql.trim()}
              onSelect={() => openSaveQueryModal(consoleId)}
            >
              <BookmarkPlus /> Save query…
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <div className="flex-1" />
        <ConsoleTransactionControls
          profileId={profile?.id ?? ""}
          running={slice.runningQuery || explaining}
          runId={slice.queryRunId}
          readState={readTransactionState}
          onBusyChange={setTransactionBusy}
        />
        <NativeSelect
          size="sm"
          aria-label="Console layout"
          value={layoutMode}
          onChange={(e) =>
            setLayoutMode(consoleId, e.target.value as typeof layoutMode)
          }
        >
          <NativeSelectOption value="split">Split</NativeSelectOption>
          <NativeSelectOption value="editor">Editor only</NativeSelectOption>
          <NativeSelectOption value="results">Results only</NativeSelectOption>
        </NativeSelect>
      </div>

      {layoutMode === "editor" && slice.queryError && (
        <Button
          variant="ghost"
          size="sm"
          className="shrink-0 justify-start text-status-danger"
          onClick={() => {
            setLayoutMode(consoleId, "split");
            setMessagesOpen(true);
          }}
        >
          Query failed · Show messages
        </Button>
      )}

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
              <strong className="font-semibold">DevKit advice:</strong> this
              connection uses the {engineMeta.name} dialect, which takes{" "}
              <code className="rounded bg-status-warning/20 px-1 py-0.5 font-mono">
                ?
              </code>{" "}
              for parameter placeholders rather than{" "}
              <code className="rounded bg-status-warning/20 px-1 py-0.5 font-mono">
                $1
              </code>
              .
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
              <span>
                Operation rejected by the database engine (safe-mode policy)
              </span>
              <span className="rounded bg-status-danger/20 px-1 py-0.5 text-code-11">
                SQLSTATE 25006
              </span>
            </div>
            <p className="text-caption-11 text-text-secondary">
              This connection is operating in{" "}
              <strong>safe-mode (read-only)</strong>. Mutating queries (INSERT,
              UPDATE, DELETE, DDL) are blocked by the database session. You can
              change this in Connection options.
            </p>
            <div className="mt-1 rounded border border-border bg-surface-base p-1.5 text-code-11 text-text-tertiary">
              {slice.queryError}
            </div>
          </div>
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="Dismiss read-only error"
            onClick={() => clearQueryError(consoleId)}
          >
            <XCircle className="size-4" />
          </Button>
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
              <strong className="font-semibold">
                The vault locked while you were working.
              </strong>{" "}
              This result stays on screen — it is already in memory. Running
              another query, or opening history, needs the vault key again.
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

      <ResizablePanelGroup
        orientation="vertical"
        key={`${showEditor}:${showResults}`}
        className="min-h-0 flex-1"
        defaultLayout={{ editor: editorSize }}
        onLayoutChanged={(layout, meta) => {
          if (meta.isUserInteraction && showEditor && showResults)
            setConsoleEditorSize(consoleId, layout.editor);
        }}
      >
        {showEditor && (
          <ResizablePanel id="editor" minSize="15%" className="flex flex-col">
            <div className="relative min-h-0 flex-1 overflow-hidden bg-surface-base py-2">
              <CodeEditor
                value={slice.sql}
                onChange={(value) => setSql(consoleId, value)}
                extensions={sqlExtension}
                wrap
              />
            </div>
          </ResizablePanel>
        )}
        {showResults ? (
          <>
            {showEditor && (
              <ResizableHandle
                withHandle
                className="bg-border-subtle aria-[orientation=horizontal]:h-1.5"
              />
            )}
            <ResizablePanel
              id="results"
              minSize="20%"
              className="flex min-h-0 flex-col overflow-hidden"
            >
              <div className="flex shrink-0 items-center gap-2 border-b border-border-subtle px-2 py-1">
                <Tabs
                  value={messagesOpen ? "messages" : String(slice.activeResult)}
                  onValueChange={(value) => {
                    setMessagesOpen(value === "messages");
                    if (value !== "messages")
                      setActiveResult(consoleId, Number(value));
                  }}
                  className="min-w-0 flex-1"
                >
                  <TabsList className="h-7 max-w-full overflow-x-auto overflow-y-hidden bg-transparent">
                    {slice.results.map((tab, index) => (
                      <TabsTrigger
                        key={`${index}-${tab.statement}`}
                        value={String(index)}
                        title={tab.statement}
                        className="h-6"
                      >
                        Result {index + 1}
                        <TabsCount>{tab.result.rowCount}</TabsCount>
                        <span
                          role="button"
                          tabIndex={0}
                          aria-label={`Close Result ${index + 1}`}
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
                          className="flex size-3.5 items-center justify-center rounded hover:bg-state-soft"
                        >
                          <X className="size-3" />
                        </span>
                      </TabsTrigger>
                    ))}
                    <TabsTrigger value="messages" className="h-6">
                      Messages
                      {slice.queryError && (
                        <span className="size-1.5 rounded-full bg-status-danger" />
                      )}
                    </TabsTrigger>
                  </TabsList>
                </Tabs>
                <ActionTooltip label="Close the result">
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => clearQueryResult(consoleId)}
                  >
                    <X />
                  </Button>
                </ActionTooltip>
              </div>
              {messagesOpen || !queryResult ? (
                <div className="min-h-0 flex-1 overflow-auto p-3 text-body-12">
                  <ConsoleRunStatus
                    outcome={outcome}
                    running={slice.runningQuery}
                    startedAt={queryStartTime}
                  />
                  {slice.queryError ? (
                    <pre className="mt-3 whitespace-pre-wrap break-words text-code-12 text-status-danger select-text">
                      {slice.queryError}
                    </pre>
                  ) : (
                    <p className="mt-3 text-text-tertiary">
                      {slice.runningQuery
                        ? "Waiting for the database…"
                        : slice.queryStatus || "No messages from this run."}
                    </p>
                  )}
                  {slice.lastStatement && (
                    <pre className="mt-4 whitespace-pre-wrap break-words text-code-12 text-text-secondary select-text">
                      {slice.lastStatement}
                    </pre>
                  )}
                </div>
              ) : (
                <div
                  className={cn(
                    "flex min-h-0 flex-1 flex-col",
                    slice.runningQuery && "pointer-events-none opacity-60",
                  )}
                >
                  <ResultGrid
                    key={`${slice.queryRunId}:${slice.activeResult}`}
                    result={queryResult}
                    widths={gridWidths}
                    columnOrder={[]}
                    onResize={setGridWidths}
                    onInspectCell={setInspectedCell}
                    status={
                      <>
                        <ConsoleRunStatus
                          outcome={outcome}
                          running={slice.runningQuery}
                          startedAt={queryStartTime}
                        />
                        {queryResult.truncated && (
                          <span className="text-status-warning">
                            row cap reached · use LIMIT/OFFSET
                          </span>
                        )}
                      </>
                    }
                    onExport={openExportModal}
                  />
                </div>
              )}
            </ResizablePanel>
          </>
        ) : null}
      </ResizablePanelGroup>

      {/* Plan Viewer Modal (Screen 22) */}
      {activePlan && (
        <QueryPlanView
          plan={activePlan}
          open={planOpen && visible}
          onClose={() => setPlanOpen(false)}
        />
      )}
    </div>
  );
}
