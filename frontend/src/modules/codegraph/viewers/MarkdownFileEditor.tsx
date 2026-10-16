import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Bold, Code, Italic, Strikethrough, TriangleAlert } from "lucide-react";
import { deserializeMd } from "@platejs/markdown";
import { KEYS } from "platejs";
import { Plate, usePlateEditor } from "platejs/react";
import { Editor, EditorContainer } from "@/components/ui/editor";
import { FloatingToolbar } from "@/components/ui/floating-toolbar";
import { LinkToolbarButton } from "@/components/ui/link-toolbar-button";
import { MarkToolbarButton } from "@/components/ui/mark-toolbar-button";
import { MarkdownFileEditorKit } from "./markdownEditorKit";
import { splitFrontMatter } from "./markdownFrontMatter";
import { markdownNorm } from "./markdownNorm";
import { losesText, spliceMarkdown } from "./markdownSplice";

let sharedNorm: ReturnType<typeof markdownNorm> | null = null;
const norm = (markdown: string) => (sharedNorm ??= markdownNorm())(markdown);

/** What one editing session starts from; replaced when the file changes under it. */
type Session = { id: number; front: string; base: string; lossy: boolean };

let sessionIds = 0;
function startSession(source: string): Session {
  const { front, body } = splitFrontMatter(source);
  return { id: ++sessionIds, front, base: body, lossy: losesText(body, norm) };
}

/**
 * A Markdown file edited the way Notes are: on the rendered page, with Plate.
 *
 * The file is never written from Plate's tree (markdownSplice.ts says why): what the person
 * changed is applied to the text the file had, so a typo fix is a one-line diff and the
 * rest of the file keeps its table padding, line wrapping and list markers. `onChange` gets
 * the whole file's new text, to be held in the buffer like any edit; Save is the buffer's.
 *
 * A file Plate would lose text from (an HTML block it cannot read, a reference-style link
 * definition) is not edited here: `fallback`, the plain text editor, is shown with the reason.
 */
export function MarkdownFileEditor({
  source,
  onChange,
  onSave,
  fallback,
}: {
  /** The buffer's text (line endings already normalised to `\n`). */
  source: string;
  onChange: (text: string) => void;
  onSave: () => void;
  fallback: ReactNode;
}) {
  const [session, setSession] = useState(() => startSession(source));
  // The last text this editor gave the buffer: a `source` that is not it came from somewhere else
  // (the disk, the text editor) and starts the session over.
  const lastEmitted = useRef(source);
  const [reformatted, setReformatted] = useState(false);

  useEffect(() => {
    if (source === lastEmitted.current) return;
    lastEmitted.current = source;
    setSession(startSession(source));
    setReformatted(false);
  }, [source]);

  // Held in refs: the parent builds both callbacks anew on every render, and an effect that depended on
  // them would hand over half-typed text each time it re-rendered.
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const onSaveRef = useRef(onSave);
  onSaveRef.current = onSave;
  const emit = useCallback((text: string, rewritten: boolean) => {
    lastEmitted.current = text;
    setReformatted(rewritten);
    onChangeRef.current(text);
  }, []);
  const save = useCallback(() => onSaveRef.current(), []);

  if (session.lossy) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        <p role="status" className="flex shrink-0 items-start gap-2 border-b border-border-subtle px-4 py-2 text-body-12 text-text-secondary">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-status-warning" aria-hidden />
          This file has content the visual editor would change or drop, so it is edited as text.
        </p>
        <div className="min-h-0 flex-1">{fallback}</div>
      </div>
    );
  }
  return (
    <div className="flex h-full min-h-0 flex-col">
      {reformatted ? (
        <p role="status" className="flex shrink-0 items-start gap-2 border-b border-border-subtle px-4 py-2 text-body-12 text-text-secondary">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-status-warning" aria-hidden />
          Saving will rewrite this file with the editor’s formatting (table padding, line breaks, list markers).
        </p>
      ) : null}
      <VisualEditor key={session.id} session={session} onEmit={emit} onSave={save} />
    </div>
  );
}

const DEBOUNCE_MS = 250;

function VisualEditor({ session, onEmit, onSave }: { session: Session; onEmit: (text: string, reformatted: boolean) => void; onSave: () => void }) {
  const { front, base } = session;
  const editor = usePlateEditor({
    plugins: MarkdownFileEditorKit,
    value: (e) => deserializeMd(e, base),
  });
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const pending = useRef(false);

  // The file's text for the editor's content now: the original while nothing differs, else the original with the edit applied.
  const flush = useCallback(() => {
    clearTimeout(timer.current);
    if (!pending.current) return;
    pending.current = false;
    const written = editor.api.markdown.serialize();
    if (written.trim() === norm(base).trim()) return onEmit(front + base, false);
    const spliced = spliceMarkdown(base, written, norm);
    onEmit(front + spliced.text, spliced.reformatted);
  }, [editor, base, front, onEmit]);

  // Leaving (View, another tab, a file reloaded under it) hands over what is typed.
  useEffect(() => flush, [flush]);

  const onValueChange = useCallback(() => {
    pending.current = true;
    clearTimeout(timer.current);
    timer.current = setTimeout(flush, DEBOUNCE_MS);
  }, [flush]);

  const toolbar = useMemo(
    () => (
      <FloatingToolbar>
        <MarkToolbarButton nodeType={KEYS.bold} tooltip="Bold">
          <Bold />
        </MarkToolbarButton>
        <MarkToolbarButton nodeType={KEYS.italic} tooltip="Italic">
          <Italic />
        </MarkToolbarButton>
        <MarkToolbarButton nodeType={KEYS.strikethrough} tooltip="Strikethrough">
          <Strikethrough />
        </MarkToolbarButton>
        <MarkToolbarButton nodeType={KEYS.code} tooltip="Code">
          <Code />
        </MarkToolbarButton>
        <LinkToolbarButton />
      </FloatingToolbar>
    ),
    [],
  );

  return (
    <Plate editor={editor} onValueChange={onValueChange}>
      <EditorContainer className="min-h-0 flex-1 overflow-y-auto" data-scroll-box>
        <div className="mx-auto w-full max-w-190 px-6 py-6">
          <Editor
            aria-label="Markdown editor"
            placeholder="Write something…"
            className="px-0 pt-0 pb-24 sm:px-0"
            onKeyDown={(e) => {
              // Save is the buffer's: the text is handed over first.
              if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
                e.preventDefault();
                flush();
                onSave();
              }
            }}
          />
        </div>
        {toolbar}
      </EditorContainer>
    </Plate>
  );
}
