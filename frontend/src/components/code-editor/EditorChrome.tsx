import { useMemo, useRef, useState, type ReactNode } from "react";
import { EditorView } from "@codemirror/view";
import { SlidersHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { LANGUAGES } from "@/components/code/languages";
import { CodeEditor, type CodeEditorProps } from "./CodeEditor";
import { EditingControls } from "./EditingControls";
import { StructureControls } from "./StructureControls";

/** Tools overlay the source, without taking a row away from the document. */
export function EditorTools({ children }: { children: ReactNode }) {
  return <div className="absolute right-5 top-2 z-10">
    <Popover>
      <PopoverTrigger asChild><Button variant="outline" size="icon-sm" className="glass-float" aria-label="Editor tools" title="Editor tools"><SlidersHorizontal /></Button></PopoverTrigger>
      <PopoverContent align="end" className="w-72">
        <span className="px-1 text-label-12 text-text-secondary">Editor tools</span>
        <div className="flex flex-wrap items-center gap-1">{children}</div>
      </PopoverContent>
    </Popover>
  </div>;
}

export function useEditorStatus(value: string) {
  const [position, setPosition] = useState({ line: 1, column: 1 });
  const extension = useMemo(() => EditorView.updateListener.of((update) => {
    if (!update.selectionSet && !update.docChanged) return;
    const head = update.state.selection.main.head;
    const line = update.state.doc.lineAt(head);
    setPosition((previous) => previous.line === line.number && previous.column === head - line.from + 1 ? previous : { line: line.number, column: head - line.from + 1 });
  }), []);
  const lines = useMemo(() => value.split(/\r\n|\n|\r/).length, [value]);
  return { extension, ...position, lines };
}

/** One quiet footer: document context on the left, editor facts on the right. */
export function EditorStatusBar({ language, status, children, trailing }: { language: string; status: { line: number; column: number; lines: number }; children?: ReactNode; trailing?: ReactNode }) {
  const label = LANGUAGES.find((entry) => entry.id === language)?.label ?? (language || "Plain text");
  return <div role="status" aria-label="File status" className="flex h-6 min-w-0 shrink-0 items-center gap-2 border-t border-border-subtle px-2 text-caption-11 text-text-tertiary">
    <div className="flex min-w-0 flex-1 items-center gap-2 overflow-hidden">{children}</div>
    <div className="flex shrink-0 items-center gap-2" title={`${label} · ${status.lines} lines · Ln ${status.line}, Col ${status.column} · UTF-8`}>
      <span className="text-caption-11-medium text-text-secondary">{label}</span>
      <span aria-hidden className="text-text-tertiary">·</span>
      <span className="tabular-nums" title="Cursor position">{status.line}:{status.column}</span>
      <span className="hidden items-center gap-2 @min-2xl/editor-chrome:flex"><span aria-hidden className="text-text-tertiary">·</span><span>{status.lines} lines</span><span aria-hidden className="text-text-tertiary">·</span><span>UTF-8</span></span>
      {trailing}
    </div>
  </div>;
}

export function SourceFileEditor({ language, ...props }: CodeEditorProps & { language: string }) {
  const viewRef = useRef<EditorView | null>(null);
  const [wrapped, setWrapped] = useState<boolean | "auto">("auto");
  const [resolvedWrap, setResolvedWrap] = useState(false);
  const status = useEditorStatus(props.value);
  const wrapStatus = useMemo(() => EditorView.updateListener.of((update) => {
    if (update.geometryChanged || update.viewportChanged) setResolvedWrap(update.view.lineWrapping);
  }), []);
  const extensions = useMemo(() => [...(props.extensions ?? []), status.extension, wrapStatus], [props.extensions, status.extension, wrapStatus]);
  return <div className="@container/editor-chrome flex h-full min-h-0 min-w-0 flex-col">
    <div className="relative min-h-0 flex-1">
      <CodeEditor {...props} extensions={extensions} wrap={wrapped} onWrapChange={setWrapped} onView={(view) => { viewRef.current = view; if (view) setResolvedWrap(view.lineWrapping); props.onView?.(view); }} />
      <EditorTools>
        <EditingControls getView={() => viewRef.current} indentStyle={props.indentStyle} readOnly={props.readOnly} />
        <StructureControls wrapped={resolvedWrap} onWrapChange={setWrapped} getView={() => viewRef.current} />
      </EditorTools>
    </div>
    <EditorStatusBar language={language} status={status} />
  </div>;
}
