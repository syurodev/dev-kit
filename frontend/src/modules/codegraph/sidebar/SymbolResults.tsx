import { useEffect, useMemo, useState, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import { shortcut } from "@/lib/shortcut";
import { searchCodeGraphSymbol, type CodeGraphSymbolMatch } from "@/bridge/codegraph";
import { errorMessage } from "@/bridge/errors";
import { LAYERS, layerOf } from "../graph/tokens";
import { NOT_SUPPORTED, symbolDisplay } from "./sidebarModel";
import { isUnsupportedProjectError } from "../codegraphErrors";

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

/**
 * `@` in the sidebar search: classes and methods in the index, including the
 * ones no endpoint reaches (Figma `CodeGraph / Sidebar — 3 Symbol search`).
 * ↑↓ move, ↵ draws the symbol's graph, ⌘↵ opens its file, esc clears.
 */
export function SymbolResults({
  query,
  onOpenGraph,
  onOpenFile,
  onClear,
}: {
  /** Without the leading @. */
  query: string;
  onOpenGraph: (m: CodeGraphSymbolMatch) => void;
  onOpenFile: (m: CodeGraphSymbolMatch) => void;
  onClear: () => void;
}) {
  // null until the index answers: "no matches" must not flash before it has.
  const [matches, setMatches] = useState<CodeGraphSymbolMatch[] | null>(null);
  const [error, setError] = useState("");
  // No analyzer reads the project: a label, not an error (see isUnsupportedProjectError).
  const [unsupported, setUnsupported] = useState(false);
  const [loading, setLoading] = useState(false);
  const [cursor, setCursor] = useState(0);
  const needle = query.trim();

  useEffect(() => {
    if (!needle) {
      setMatches(null);
      setError("");
      return;
    }
    let live = true;
    setLoading(true);
    const t = window.setTimeout(() => {
      searchCodeGraphSymbol(needle)
        .then((m) => live && (setMatches(m), setError(""), setUnsupported(false), setCursor(0)))
        .catch((e) => {
          if (!live) return;
          if (isUnsupportedProjectError(e)) {
            setUnsupported(true);
            setError("");
          } else {
            setError(errorMessage(e));
          }
        })
        .finally(() => live && setLoading(false));
    }, DEBOUNCE_MS);
    return () => {
      live = false;
      window.clearTimeout(t);
    };
  }, [needle]);

  const classes = useMemo(() => (matches ?? []).filter((m) => m.kind === "class"), [matches]);
  const methods = useMemo(() => (matches ?? []).filter((m) => m.kind !== "class"), [matches]);
  const ordered = useMemo(() => [...classes, ...methods], [classes, methods]);

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

  const section = (label: string, list: CodeGraphSymbolMatch[], offset: number) =>
    list.length > 0 ? (
      <>
        <div className="px-2 pt-1.5 pb-1 text-label-11-caps text-text-tertiary">
          {label} · {list.length}
        </div>
        {list.map((m, i) => {
          const { name, detail } = symbolDisplay(m);
          const at = offset + i;
          return (
            <button
              key={m.nodeId}
              type="button"
              role="option"
              aria-selected={at === cursor}
              onMouseEnter={() => setCursor(at)}
              onClick={(e) => (e.metaKey || e.ctrlKey ? onOpenFile : onOpenGraph)(m)}
              className={cn(
                "flex w-full shrink-0 items-start gap-2 rounded-md border px-2 py-1.5 text-left",
                at === cursor ? "border-border-subtle bg-surface-raised" : "border-transparent",
              )}
            >
              <span
                className="mt-1.5 size-2 shrink-0 rounded-full"
                style={{ backgroundColor: `var(${LAYERS[layerOf(m.layer)].cssVar})` }}
                aria-label={LAYERS[layerOf(m.layer)].label.toLowerCase()}
              />
              <span className="flex min-w-0 flex-1 flex-col gap-px">
                <span className="truncate text-body-13">{highlight(name, needle)}</span>
                <span className="truncate text-code-11 text-text-tertiary">{detail}</span>
              </span>
            </button>
          );
        })}
      </>
    ) : null;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto" role="listbox" aria-label="Symbols">
        {section("Classes", classes, 0)}
        {section("Methods", methods, classes.length)}
        {!needle ? (
          <p className="px-2 py-3 text-body-12 text-text-tertiary">Type a class or method name after @.</p>
        ) : unsupported ? (
          <p role="status" className="px-2 py-3 text-body-12 text-text-tertiary">
            {NOT_SUPPORTED} — no analyzer reads this project, so it has no classes or methods to search.
          </p>
        ) : error ? (
          <p role="alert" className="px-2 py-3 text-body-12 text-status-danger">{error}</p>
        ) : matches !== null && !loading && ordered.length === 0 ? (
          <p className="px-2 py-3 text-body-12 text-text-tertiary">No classes or methods match “{needle}”.</p>
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
