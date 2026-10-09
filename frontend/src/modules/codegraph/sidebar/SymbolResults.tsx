import { useEffect, useMemo, useState, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import { shortcut } from "@/lib/shortcut";
import { searchWorkspaceSymbols } from "@/bridge/workspaceCode";
import { errorMessage } from "@/bridge/errors";
import type { CodeGraphSymbolMatch } from "@/bridge/codegraph";
import { LAYERS, layerOf } from "../graph/tokens";
import { NOT_SUPPORTED, symbolDisplay } from "./sidebarModel";

/** A query past this many characters waits this long for the next keystroke. */
const DEBOUNCE_MS = 180;

/** The part of `text` matching `needle`, drawn primary; the rest secondary. */
function highlight(text: string, needle: string): ReactNode {
  const i = needle ? text.toLowerCase().indexOf(needle.toLowerCase()) : -1;
  if (i === -1) return <span className="text-text-secondary">{text}</span>;
  return (
    <>
      <span className="text-text-secondary">{text.slice(0, i)}</span>
      <span className="text-text-primary">{text.slice(i, i + needle.length)}</span>
      <span className="text-text-secondary">{text.slice(i + needle.length)}</span>
    </>
  );
}

export type SymbolHit = { folderId: string; match: CodeGraphSymbolMatch };

/**
 * `@` in the sidebar search: classes and methods in the indexes of the folders in
 * view, including the ones no endpoint reaches (Figma `CodeGraph / Sidebar — 3
 * Symbol search`). Each folder's own index answers — read from disk, so nothing
 * is opened to ask — and a hit names its folder when there is more than one.
 * ↑↓ move, ↵ draws the symbol's graph, ⌘↵ opens its file, esc clears.
 */
export function SymbolResults({
  workspaceId,
  folderIds,
  labels,
  query,
  onOpenGraph,
  onOpenFile,
  onClear,
}: {
  workspaceId: string;
  /** The folders in the sidebar's scope. */
  folderIds: string[];
  labels: Record<string, string>;
  /** Without the leading @. */
  query: string;
  onOpenGraph: (hit: SymbolHit) => void;
  onOpenFile: (hit: SymbolHit) => void;
  onClear: () => void;
}) {
  // null until the index answers: "no matches" must not flash before it has.
  const [hits, setHits] = useState<SymbolHit[] | null>(null);
  const [notes, setNotes] = useState<Array<{ folderId: string; state: string }>>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [cursor, setCursor] = useState(0);
  const needle = query.trim();
  const scopeKey = folderIds.join(",");

  useEffect(() => {
    if (!needle) {
      setHits(null);
      setError("");
      return;
    }
    let live = true;
    setLoading(true);
    const t = window.setTimeout(() => {
      searchWorkspaceSymbols(workspaceId, folderIds, needle)
        .then((folders) => {
          if (!live) return;
          setHits(folders.flatMap((f) => f.matches.map((match) => ({ folderId: f.folderId, match }))));
          setNotes(folders.filter((f) => f.state !== "indexed").map((f) => ({ folderId: f.folderId, state: f.state })));
          setError("");
          setCursor(0);
        })
        .catch((e) => live && setError(errorMessage(e)))
        .finally(() => live && setLoading(false));
    }, DEBOUNCE_MS);
    return () => {
      live = false;
      window.clearTimeout(t);
    };
    // folderIds is compared by value through scopeKey.
  }, [needle, workspaceId, scopeKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const classes = useMemo(() => (hits ?? []).filter((h) => h.match.kind === "class"), [hits]);
  const methods = useMemo(() => (hits ?? []).filter((h) => h.match.kind !== "class"), [hits]);
  const ordered = useMemo(() => [...classes, ...methods], [classes, methods]);
  const several = folderIds.length > 1;

  // The search field keeps focus while the list is walked, so the keys are
  // read from the window; typing still goes to the field.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        setCursor((c) => Math.max(0, Math.min(ordered.length - 1, c + (e.key === "ArrowDown" ? 1 : -1))));
      } else if (e.key === "Enter" && ordered[cursor]) {
        e.preventDefault();
        (e.metaKey || e.ctrlKey ? onOpenFile : onOpenGraph)(ordered[cursor]);
      } else if (e.key === "Escape") {
        onClear();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [ordered, cursor, onOpenGraph, onOpenFile, onClear]);

  const section = (label: string, list: SymbolHit[], offset: number) =>
    list.length > 0 ? (
      <>
        <div className="px-2 pt-1.5 pb-1 text-label-11-caps text-text-tertiary">
          {label} · {list.length}
        </div>
        {list.map((hit, i) => {
          const { name, detail } = symbolDisplay(hit.match);
          const at = offset + i;
          return (
            <button
              key={`${hit.folderId}\u0000${hit.match.nodeId}`}
              type="button"
              role="option"
              aria-selected={at === cursor}
              onMouseEnter={() => setCursor(at)}
              onClick={(e) => (e.metaKey || e.ctrlKey ? onOpenFile : onOpenGraph)(hit)}
              className={cn(
                "flex w-full shrink-0 items-start gap-2 rounded-md border px-2 py-1.5 text-left",
                at === cursor ? "border-border-subtle bg-surface-raised" : "border-transparent",
              )}
            >
              <span
                className="mt-1.5 size-2 shrink-0 rounded-full"
                style={{ backgroundColor: `var(${LAYERS[layerOf(hit.match.layer)].cssVar})` }}
                aria-label={LAYERS[layerOf(hit.match.layer)].label.toLowerCase()}
              />
              <span className="flex min-w-0 flex-1 flex-col gap-px">
                <span className="truncate text-body-13">{highlight(name, needle)}</span>
                <span className="truncate text-code-11 text-text-tertiary">{detail}</span>
              </span>
              {several ? <span className="max-w-24 shrink-0 truncate text-code-11 text-text-tertiary" title={labels[hit.folderId]}>{labels[hit.folderId]}</span> : null}
            </button>
          );
        })}
      </>
    ) : null;

  const unsupportedOnly = notes.length > 0 && notes.length === folderIds.length && notes.every((n) => n.state === "unsupported");

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto" role="listbox" aria-label="Symbols">
        {section("Classes", classes, 0)}
        {section("Methods", methods, classes.length)}
        {!needle ? (
          <p className="px-2 py-3 text-body-12 text-text-tertiary">Type a class or method name after @.</p>
        ) : unsupportedOnly ? (
          <p role="status" className="px-2 py-3 text-body-12 text-text-tertiary">
            {NOT_SUPPORTED} — no analyzer reads {folderIds.length === 1 ? "this folder" : "these folders"}, so there are no classes or methods to search.
          </p>
        ) : error ? (
          <p role="alert" className="px-2 py-3 text-body-12 text-status-danger">{error}</p>
        ) : hits !== null && !loading && ordered.length === 0 ? (
          <p className="px-2 py-3 text-body-12 text-text-tertiary">No classes or methods match “{needle}”.</p>
        ) : null}
        {hits !== null && notes.length > 0 && !unsupportedOnly ? (
          <p className="px-2 pt-2 text-caption-11 text-text-tertiary">
            Not searched: {notes.map((n) => `${labels[n.folderId] ?? "a folder"} (${n.state === "unsupported" ? "files only" : n.state === "not-indexed" ? "not indexed" : n.state})`).join(", ")}.
          </p>
        ) : null}
      </div>
      <div className="flex shrink-0 flex-wrap gap-x-3 gap-y-0.5 px-2 pt-2 text-code-11 text-text-tertiary">
        <span>↑↓ move</span>
        <span>↵ open graph</span>
        <span>{shortcut("Enter")} open file</span>
        <span>esc clear</span>
      </div>
    </div>
  );
}
