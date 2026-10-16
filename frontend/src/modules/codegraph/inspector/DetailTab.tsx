import { FileIcon } from "@/components/file-icon/FileIcon";
import { useMemo } from "react";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { dedent } from "@/components/code-editor/dedent";
import { Button } from "@/components/ui/button";
import { lineNumbers } from "@codemirror/view";
import { languageForPath } from "@/components/code/languages";
import { useLanguageExtension } from "@/components/code/useLanguageExtension";
import type { CodeGraphCodeSnippet } from "@/bridge/codegraph";

export type DetailTabProps = {
  snippet: CodeGraphCodeSnippet | null;
  loading: boolean;
  error: string;
  /** Absent for a node with no source yet (a plan node still not_implemented). */
  onEdit?: () => void;
};

/**
 * Figma Node Panel `body` (331:456): FQN in a padded block, then
 * "SOURCE · File.java:24" over a surface-well code card. The node's name,
 * layer and kind are the panel header's job — repeating them here would say
 * the same thing twice in 60 vertical pixels.
 */
export function DetailTab({ snippet, loading, error, onEdit }: DetailTabProps) {
  // The snippet is cut from the middle of a file, so CodeMirror's own numbering
  // would start at 1 and disagree with the ":24" beside it.
  const language = useLanguageExtension(languageForPath(snippet?.filePath));
  const extensions = useMemo(
    () =>
      snippet
        ? [...language, lineNumbers({ formatNumber: (n) => String(n + snippet.line - 1) })]
        : [],
    [snippet, language],
  );

  // A method cut out of its class still carries the class's indentation. In
  // this panel — narrow by design — those are columns the code needed, and with
  // wrapping on they also push every continuation row further right. Display
  // only: the buffer is read-only, and Edit opens the real file.
  const source = useMemo(() => (snippet ? dedent(snippet.source) : ""), [snippet]);

  if (loading) return <p className="p-5 text-body-13 text-text-tertiary">Loading source…</p>;
  if (error) return <p className="p-5 text-body-13 text-status-danger">{error}</p>;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3.5 px-5 pt-4 pb-4.5">
      {snippet?.fqn && (
        <div className="flex flex-col gap-1 rounded-md p-2.5">
          <span className="text-label-11-caps text-text-tertiary">FQN</span>
          <span className="truncate text-code-11 text-text-secondary">{snippet.fqn}</span>
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col gap-1.5">
        <div className="flex items-center gap-2">
          {snippet ? <FileIcon path={snippet.filePath} className="size-3.5" /> : null}
          <span className="min-w-0 flex-1 truncate text-label-11-caps text-text-tertiary">
            {snippet ? `Source · ${snippet.filePath.split("/").pop()}:${snippet.line}` : "Source"}
          </span>
          {snippet && onEdit && (
            <Button variant="outline" size="xs" className="shrink-0 bg-surface-raised" onClick={onEdit}>
              Edit
            </Button>
          )}
        </div>

        {snippet ? (
          // Boundaries come from the AST the indexer already walked, so the
          // snippet is exactly the method or class — not a guessed ±N lines.
          <div className="min-h-0 flex-1 overflow-hidden rounded-md border border-border-subtle bg-surface-well">
            <CodeEditor
              value={source}
              onChange={() => {}}
              extensions={extensions}
              readOnly
              wrap
            />
          </div>
        ) : (
          <p className="rounded-md border border-border-subtle bg-surface-well px-3 py-2 text-body-12 text-text-tertiary">
            Not implemented yet — there is no file to open.
          </p>
        )}
      </div>
    </div>
  );
}
