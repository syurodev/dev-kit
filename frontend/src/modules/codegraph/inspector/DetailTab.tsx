import { useMemo } from "react";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { Button } from "@/components/ui/button";
import { lineNumbers } from "@codemirror/view";
import { languageExtensionForPath } from "../editorLanguage";
import type { CodeGraphCodeSnippet } from "@/bridge/codegraph";

export type DetailTabProps = {
  snippet: CodeGraphCodeSnippet | null;
  loading: boolean;
  error: string;
  /** Absent for a node with no source yet (a plan node still not_implemented). */
  onEdit?: () => void;
};

/**
 * One node's FQN and source, as the Detail tab of the right panel.
 *
 * The node's name, layer and kind are the panel header's job now — repeating
 * them here would say the same thing twice in 60 vertical pixels.
 */
export function DetailTab({ snippet, loading, error, onEdit }: DetailTabProps) {
  // The snippet is cut from the middle of a file, so CodeMirror's own numbering
  // would start at 1 and disagree with the ":24" beside it.
  const extensions = useMemo(
    () =>
      snippet
        ? [
            ...languageExtensionForPath(snippet.filePath),
            lineNumbers({ formatNumber: (n) => String(n + snippet.line - 1) }),
          ]
        : [],
    [snippet],
  );

  if (loading) return <p className="p-4 text-sm text-muted-foreground">Loading source…</p>;
  if (error) return <p className="p-4 text-sm text-destructive">{error}</p>;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 p-3">
      {snippet?.fqn && (
        <div className="flex flex-col gap-1">
          <span className="font-mono text-[10px] tracking-[0.08em] text-text-tertiary">FQN</span>
          <span className="truncate rounded-lg bg-surface-tinted px-2.5 py-1.5 font-mono text-[11px] text-text-secondary">
            {snippet.fqn}
          </span>
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col gap-1">
        <div className="flex items-center gap-2">
          <span className="min-w-0 flex-1 truncate font-mono text-[10px] tracking-[0.08em] text-text-tertiary">
            {snippet ? `SOURCE · ${snippet.filePath.split("/").pop()}:${snippet.line}` : "SOURCE"}
          </span>
          {snippet && onEdit && (
            <Button
              variant="secondary"
              size="sm"
              className="h-6 shrink-0 px-2 text-xs"
              onClick={onEdit}
            >
              Edit
            </Button>
          )}
        </div>

        {snippet ? (
          // Boundaries come from the AST the indexer already walked, so the
          // snippet is exactly the method or class — not a guessed ±N lines.
          <div className="min-h-0 flex-1 overflow-hidden rounded-lg bg-surface-tinted">
            <CodeEditor
              value={snippet.source}
              onChange={() => {}}
              extensions={extensions}
              readOnly
              wrap
            />
          </div>
        ) : (
          <p className="rounded-lg bg-surface-tinted px-2.5 py-2 text-xs text-text-tertiary">
            Not implemented yet — there is no file to open.
          </p>
        )}
      </div>
    </div>
  );
}
