import { setDiagnostics } from "@codemirror/lint";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EditorView, keymap } from "@codemirror/view";
import { Prec } from "@codemirror/state";
import { Braces, Check, ChevronDown, CircleAlert, Code, X } from "lucide-react";
import { CodeEditor, type CodeEditorProps } from "@/components/code-editor/CodeEditor";
import { StructureControls } from "@/components/code-editor/StructureControls";
import { useLanguageExtension } from "@/components/code/useLanguageExtension";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ResizablePanel, ResizablePanelGroup, ResizableHandle } from "@/components/ui/resizable";
import { requestYaml } from "./client";
import { activeYamlRange, applyYamlEdit, yamlAssistance } from "./extension";
import { SCHEMA_LABELS, type Analysis, type Edit, type SchemaChoice, type SqlBlock } from "./types";

// SQL edits are independent of view mounting: changing tabs or closing the
// pane keeps the draft until Apply or the person's explicit Discard action.
type Draft = { block: SqlBlock; code: string; open: boolean };
const drafts = new Map<string, Draft>();
const choices = new Map<string, SchemaChoice>();
const wraps = new Map<string, boolean>();
const labelOf = (path: (string | number)[]) => path.map((part) => typeof part === "number" ? `[${part}]` : part).join(" › ") || "Document";

export function YamlEditor({ file, documentKey, active = true, ...props }: Omit<CodeEditorProps, "wrap" | "onWrapChange"> & { file: string; documentKey: string; active?: boolean }) {
  const viewRef = useRef<EditorView | null>(null);
  const alive = useRef(true);
  const [schema, setSchema] = useState<SchemaChoice>(() => choices.get(documentKey) ?? "auto");
  const schemaRef = useRef(schema); schemaRef.current = schema;
  const [wrapped, setWrapped] = useState(() => wraps.get(documentKey) ?? true);
  const [analysis, setAnalysis] = useState<Analysis | null>(null);
  const [draft, setDraftState] = useState<Draft | null>(() => drafts.get(documentKey) ?? null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const generation = useRef(0);
  const setDraft = (next: Draft | null) => {
    if (next) drafts.set(documentKey, next); else drafts.delete(documentKey);
    setDraftState(next);
  };
  const setWrap = (next: boolean) => { wraps.set(documentKey, next); setWrapped(next); };
  const receive = useCallback((next: Analysis) => {
    if (!alive.current) return;
    setAnalysis(next);
    const view = viewRef.current;
    if (view) view.dispatch(setDiagnostics(view.state, next.problems), { effects: activeYamlRange.of(next.active) });
  }, []);
  const refreshRef = useRef<() => void>(() => {});
  refreshRef.current = () => {
    const view = viewRef.current;
    if (!view || !active) return;
    const text = view.state.doc.toString(), position = view.state.selection.main.head;
    const choice = schemaRef.current, token = ++generation.current;
    requestYaml({ type: "analyze", text, position, file, schema: choice }).then((result) => {
      if (alive.current && token === generation.current && viewRef.current === view && view.state.doc.toString() === text && schemaRef.current === choice) receive(result);
    }).catch((error) => {
      if (alive.current && token === generation.current) {
        const message = error instanceof Error ? error.message : String(error);
        receive({ path: [], active: null, sql: null, schema: "Syntax only", notice: message, problems: [{ from: 0, to: 0, severity: "warning", message, source: "YAML assistance" }] });
      }
    });
  };
  const schedule = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    ++generation.current;
    timer.current = setTimeout(() => refreshRef.current(), 150);
  }, []);
  const extensions = useMemo(() => [
    ...(props.extensions ?? []),
    yamlAssistance({ file, schema }),
    EditorView.updateListener.of((update) => {
      if (update.docChanged || update.selectionSet) schedule();
    }),
    Prec.highest(keymap.of([{ key: "Alt-Shift-f", run: () => { formatRef.current(false); return true; } }])),
  ], [props.extensions, file, schema, receive, schedule]);
  const onView = useCallback((view: EditorView | null) => {
    viewRef.current = view;
    props.onView?.(view);
    if (view) schedule();
  }, [props.onView, schedule]);
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; ++generation.current; if (timer.current) clearTimeout(timer.current); };
  }, []);
  useEffect(() => { schedule(); }, [schema, active, schedule]);
  const apply = (view: EditorView, source: string, edit: Edit) => {
    if (viewRef.current !== view || view.state.doc.toString() !== source) throw new Error("The YAML changed during this action. Try again with the current text.");
    applyYamlEdit(view, edit);
  };
  const format = async (selection: boolean) => {
    const view = viewRef.current;
    if (!view || busy) return;
    const text = view.state.doc.toString(), range = view.state.selection.main;
    if (selection && range.empty) { setError("Select complete YAML fields to format a selection."); return; }
    setBusy(true); setError("");
    try {
      const edit = await requestYaml({ type: "format", text, file, schema, selection: selection ? { from: range.from, to: range.to } : undefined });
      if (alive.current) apply(view, text, edit);
    } catch (error) { if (alive.current) setError(error instanceof Error ? error.message : String(error)); }
    finally { if (alive.current) setBusy(false); }
  };
  const formatRef = useRef<(selection: boolean) => void>(() => {}); formatRef.current = (selection) => { void format(selection); };
  const openSql = async () => {
    if (draft) { setDraft({ ...draft, open: !draft.open }); return; }
    const view = viewRef.current;
    if (!view) return;
    setError("");
    try {
      const source = view.state.doc.toString();
      const result = await requestYaml({ type: "analyze", text: source, position: view.state.selection.main.head, file, schema });
      if (!alive.current || view.state.doc.toString() !== source) return;
      if (!result.sql) { setError("Place the cursor in a multiline sql: | field (or a .sql field) to edit its SQL."); return; }
      setDraft({ block: result.sql, code: result.sql.value, open: true });
    } catch (error) { if (alive.current) setError(String(error)); }
  };
  const applySql = async () => {
    const view = viewRef.current;
    if (!view || !draft || busy) return;
    const text = view.state.doc.toString();
    setBusy(true); setError("");
    try {
      const edit = await requestYaml({ type: "applySql", text, file, schema, block: draft.block, value: draft.code });
      if (alive.current) { apply(view, text, edit); setDraft(null); }
    } catch (error) { if (alive.current) setError(error instanceof Error ? error.message : String(error)); }
    finally { if (alive.current) setBusy(false); }
  };
  const problems = analysis?.problems ?? [];
  const pathLabel = labelOf(analysis?.path ?? []);
  return <div className="flex h-full min-h-0 min-w-0 flex-col" data-testid="yaml-editor">
    <div className="flex shrink-0 flex-wrap items-center gap-1 border-b border-border-subtle px-2 py-1">
      <StructureControls wrapped={wrapped} onWrapChange={setWrap} getView={() => viewRef.current} />
      <DropdownMenu><DropdownMenuTrigger asChild><Button size="xs" variant="ghost" disabled={busy}><Braces />Format<ChevronDown /></Button></DropdownMenuTrigger>
        <DropdownMenuContent><DropdownMenuItem onSelect={() => void format(false)}>Format document</DropdownMenuItem><DropdownMenuItem onSelect={() => void format(true)}>Format selection</DropdownMenuItem></DropdownMenuContent>
      </DropdownMenu>
      <ActionTooltip label={draft && !draft.open ? "Resume SQL draft" : "Edit SQL block"}><Button size="xs" variant={draft?.open ? "secondary" : "ghost"} onClick={() => void openSql()}><Code />SQL{draft && draft.code !== draft.block.value ? " · draft" : ""}</Button></ActionTooltip>
    </div>
    {error ? <div role="alert" className="flex shrink-0 items-start gap-2 bg-status-danger/8 px-3 py-2 text-body-12 text-status-danger"><CircleAlert className="mt-0.5 size-3.5 shrink-0" /><span className="min-w-0 flex-1 wrap-anywhere">{error}</span><Button variant="ghost" size="icon-xs" aria-label="Dismiss YAML message" onClick={() => setError("")}><X /></Button></div> : null}
    <ResizablePanelGroup orientation="vertical" className="min-h-0 flex-1">
      <ResizablePanel id="yaml-source" defaultSize="60%" minSize="20%"><CodeEditor {...props} onView={onView} extensions={extensions} wrap={wrapped} onWrapChange={setWrap} /></ResizablePanel>
      {draft?.open ? <><ResizableHandle withHandle /><ResizablePanel id="yaml-sql" defaultSize="40%" minSize="20%"><SqlEditor draft={draft} busy={busy} onChange={(code) => setDraft({ ...draft, code })} onApply={() => void applySql()} onClose={() => setDraft({ ...draft, open: false })} onDiscard={() => setDraft(null)} /></ResizablePanel></> : null}
    </ResizablePanelGroup>
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-t border-border-subtle px-2 py-1 text-caption-11 text-text-tertiary">
      <span className="min-w-0 flex-1 truncate" title={pathLabel}>{pathLabel}</span>
      <Select value={schema} onValueChange={(value) => { const choice = value as SchemaChoice; choices.set(documentKey, choice); schemaRef.current = choice; ++generation.current; setSchema(choice); if (viewRef.current) viewRef.current.dispatch(setDiagnostics(viewRef.current.state, [])); setAnalysis(null); setError(""); }}><SelectTrigger size="sm" aria-label="YAML schema" className="h-6 w-auto max-w-48 px-2 text-caption-11"><SelectValue /></SelectTrigger><SelectContent>{Object.entries(SCHEMA_LABELS).map(([id, label]) => <SelectItem key={id} value={id}>{label}</SelectItem>)}</SelectContent></Select>
      <DropdownMenu><DropdownMenuTrigger asChild><Button size="xs" variant="ghost" className="h-6 px-2" disabled={!problems.length} aria-label="YAML problems">{problems.length ? <CircleAlert /> : <Check />}{!analysis ? "Checking…" : problems.length ? `${problems.length} ${problems.length === 1 ? "problem" : "problems"}` : "No problems"}</Button></DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="max-h-72 w-96 max-w-[90vw] overflow-y-auto">{problems.map((problem, index) => <DropdownMenuItem key={index} className="h-auto whitespace-normal" onSelect={() => { const view = viewRef.current; if (view) { const pos = Math.min(problem.from, view.state.doc.length); view.dispatch({ selection: { anchor: pos }, effects: EditorView.scrollIntoView(pos, { y: "center" }) }); view.focus(); } }}><span className="text-body-12">Line {viewRef.current?.state.doc.lineAt(Math.min(problem.from, viewRef.current.state.doc.length)).number ?? 1}: {problem.message}</span></DropdownMenuItem>)}</DropdownMenuContent>
      </DropdownMenu>
      {analysis?.notice ? <span className="w-full text-status-warning">{analysis.notice}</span> : analysis && schema === "auto" ? <span className="max-w-64 truncate" title={analysis.schema}>{analysis.schema}</span> : null}
    </div>
  </div>;
}

function SqlEditor({ draft, busy, onChange, onApply, onClose, onDiscard }: { draft: Draft; busy: boolean; onChange: (value: string) => void; onApply: () => void; onClose: () => void; onDiscard: () => void }) {
  const extensions = useLanguageExtension("sql");
  return <div className="flex h-full min-h-0 flex-col" data-testid="yaml-sql-pane">
    <div className="flex shrink-0 flex-wrap items-center gap-1 border-b border-border-subtle px-2 py-1">
      <Code className="size-3.5 text-text-secondary" /><span title={draft.block.label} className="min-w-0 flex-1 truncate text-code-12 text-text-secondary">{draft.block.label}</span>
      <Button size="xs" variant="ghost" disabled={busy} onClick={onDiscard}>Discard</Button>
      <Button size="xs" variant="secondary" disabled={busy} onClick={onApply}>Apply to YAML</Button>
      <ActionTooltip label="Close SQL pane (keep draft)"><Button size="icon-xs" variant="ghost" onClick={onClose}><X /></Button></ActionTooltip>
    </div>
    <div className="min-h-0 flex-1"><CodeEditor value={draft.code} onChange={onChange} extensions={extensions} readOnly={busy} wrap /></div>
    <p className="shrink-0 px-3 py-1 text-caption-11 text-text-tertiary">Apply updates this YAML block. Save the file from the main editor.</p>
  </div>;
}
