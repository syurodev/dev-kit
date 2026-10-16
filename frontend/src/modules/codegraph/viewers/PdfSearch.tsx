import { useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import { errorMessage, isCancelError, toAppError } from "@/bridge/errors";
import { pdfSearch, type PdfSearchResult } from "@/bridge/workspaceCode";
import { Input } from "@/components/ui/input";

type State = { status: "idle" } | { status: "running" } | { status: "error"; message: string } | { status: "done"; query: string; result: PdfSearchResult };

/**
 * Search in a PDF's text. The hits are pages with a line of context, and a
 * click goes to the page: PDFium finds the words and Go reports where, but the
 * viewer draws pictures, so the match itself is not marked on the page. A scan
 * has no text to search and says so.
 */
export function PdfSearch({
  workspaceId,
  folderId,
  path,
  revision,
  onGo,
  onClose,
  onChanged,
}: {
  workspaceId: string;
  folderId: string;
  path: string;
  revision: string;
  onGo: (page: number) => void;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [query, setQuery] = useState("");
  const [state, setState] = useState<State>({ status: "idle" });
  const running = useRef<{ cancel: () => void } | null>(null);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    input.current?.focus();
    return () => running.current?.cancel();
  }, []);

  const run = () => {
    const q = query.trim();
    running.current?.cancel();
    if (!q) {
      setState({ status: "idle" });
      return;
    }
    setState({ status: "running" });
    const call = pdfSearch(workspaceId, folderId, path, revision, q);
    running.current = call;
    call.then(
      (result) => {
        if (running.current === call) setState({ status: "done", query: q, result });
      },
      (e: unknown) => {
        if (running.current !== call || isCancelError(e)) return;
        if (toAppError(e).code === "pdf_changed") onChanged();
        else setState({ status: "error", message: errorMessage(e) });
      },
    );
  };

  return (
    <div className="flex shrink-0 flex-col gap-1.5 border-b border-border-subtle px-3 py-2" role="search">
      <div className="flex items-center gap-2">
        <Input
          ref={input}
          inputSize="sm"
          value={query}
          placeholder="Search in this PDF"
          aria-label="Search in this PDF"
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") run();
            if (e.key === "Escape") onClose();
          }}
        />
        {state.status === "running" ? <Loader2 className="size-4 shrink-0 animate-spin text-text-tertiary motion-reduce:animate-none" aria-label="Searching" /> : null}
      </div>
      {state.status === "error" ? (
        <p role="alert" className="text-body-12 text-status-danger">{state.message}</p>
      ) : null}
      {state.status === "done" ? <Results state={state} onGo={onGo} /> : null}
    </div>
  );
}

function Results({ state, onGo }: { state: Extract<State, { status: "done" }>; onGo: (page: number) => void }) {
  const { result } = state;
  const notes: string[] = [];
  if (result.truncated) notes.push(`Showing the first ${result.hits.length} matches.`);
  if (!result.complete) notes.push("The search ran out of time before the last pages.");
  if (result.pagesWithoutText.length > 0) {
    const n = result.pagesWithoutText.length;
    notes.push(`${n} page${n === 1 ? " has" : "s have"} no text (scanned) and cannot be searched.`);
  }
  return (
    <div className="flex flex-col gap-1" role="status">
      {result.hits.length === 0 ? (
        <p className="text-body-12 text-text-secondary">No matches for “{state.query}”.</p>
      ) : (
        <ul className="max-h-40 overflow-y-auto" aria-label={`${result.hits.length} matches`}>
          {result.hits.map((hit, i) => (
            <li key={`${hit.page}:${i}`}>
              <button
                type="button"
                onClick={() => onGo(hit.page)}
                className="flex w-full items-baseline gap-2 rounded-sm px-1.5 py-1 text-left hover:bg-state-soft focus-visible:ring-1 focus-visible:ring-status-info focus-visible:outline-none"
              >
                <span className="w-12 shrink-0 text-code-11 text-text-tertiary tabular-nums">p. {hit.page}</span>
                <span className="min-w-0 flex-1 truncate text-body-12 text-text-primary">{hit.snippet}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {notes.map((note) => (
        <p key={note} className="text-caption-11 text-text-tertiary">{note}</p>
      ))}
    </div>
  );
}
