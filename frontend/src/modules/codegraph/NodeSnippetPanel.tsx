import { useMemo } from "react";
import { lineNumbers } from "@codemirror/view";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { Button } from "@/components/ui/button";
import { languageExtensionForPath } from "./editorLanguage";
import { shortLabel } from "./graph/labels";
import type { CodeGraphCodeSnippet } from "@/bridge/codegraph";

export type NodeSnippetPanelProps = {
  snippet: CodeGraphCodeSnippet | null;
  loading: boolean;
  error: string;
  /** Absent for a node with no source yet (a plan node still not_implemented). */
  onEdit?: () => void;
};

/**
 * Read-only preview of one node's source, shown in the shared SplitPanel.
 *
 * Reuses the Code editor primitive with readOnly rather than a bespoke viewer —
 * preview and edit are two modes of one component, per codegraph.mdx § Click
 * node. "Edit" hands off to the real file-editing flow (Ctrl+S writes to disk).
 */
export function NodeSnippetPanel({ snippet, loading, error, onEdit }: NodeSnippetPanelProps) {
  // The snippet is cut out of the middle of a file, so CodeMirror's own
  // numbering would start at 1 and quietly disagree with the ":26" in the
  // header above it. Offsetting by the real start line makes every number in
  // the gutter one you can actually navigate to.
  //
  // Passing lineNumbers() again alongside basicSetup's is safe: its gutter is
  // a single shared extension value, which CodeMirror dedupes, and only this
  // config sets formatNumber, so there is nothing for the facet to conflict on.
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

  if (loading) {
    return <p className="p-4 text-sm text-muted-foreground">Loading source…</p>;
  }
  if (error) {
    return <p className="p-4 text-sm text-destructive">{error}</p>;
  }
  if (!snippet) {
    return (
      <p className="p-4 text-sm text-muted-foreground">
        This node has no source yet — nothing to preview.
      </p>
    );
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex items-center justify-between gap-2 border-b px-3 py-2 pr-12">
        <span className="flex min-w-0 flex-col">
          <span
            title={snippet.fqn || snippet.filePath}
            className="truncate text-sm font-semibold text-text-primary"
          >
            {snippet.fqn ? shortLabel(snippet.fqn, "method") : snippet.filePath.split("/").pop()}
          </span>
          <span
            title={`${snippet.filePath}:${snippet.line}`}
            className="truncate font-mono text-[11px] text-text-tertiary"
          >
            {snippet.filePath}:{snippet.line}
          </span>
        </span>
        {onEdit && (
          <Button variant="secondary" size="sm" className="shrink-0" onClick={onEdit}>
            Edit
          </Button>
        )}
      </div>
      <div className="flex-1 overflow-hidden">
        {/* Boundaries come from the AST the indexer already walked, so the
            snippet is exactly the method or class — not a guessed ±N lines. */}
        <CodeEditor
          value={snippet.source}
          onChange={() => {}}
          extensions={extensions}
          readOnly
          wrap
        />
      </div>
    </div>
  );
}
