import * as React from "react";

/**
 * Which summary sections the person has folded. One list for the docked card
 * and the corner popover (both are mounted), remembered across sessions and
 * restarts: a person who never wants Environment folds it once. Storage can be
 * unavailable or hold junk, so every read and write is guarded and the
 * sections simply start open.
 */
const KEY = "devkit.agenthub.summary.closed";

let closed: ReadonlySet<string> | null = null;
const listeners = new Set<() => void>();

function read(): ReadonlySet<string> {
  if (closed) return closed;
  let names: string[] = [];
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(KEY) ?? "[]");
    if (Array.isArray(parsed)) names = parsed.filter((n): n is string => typeof n === "string");
  } catch {
    // Unavailable or malformed: everything starts open.
  }
  closed = new Set(names);
  return closed;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Folds or opens one section for everyone listening. */
export function setSummarySectionOpen(name: string, open: boolean): void {
  const next = new Set(read());
  if (open) next.delete(name);
  else next.add(name);
  closed = next;
  try {
    localStorage.setItem(KEY, JSON.stringify([...next]));
  } catch {
    // Not remembered; still folded for this window.
  }
  for (const listener of listeners) listener();
}

/** Forgets what was read, so a test can start from storage's own state. */
export function resetSummaryFold(): void {
  closed = null;
  for (const listener of listeners) listener();
}

/** Whether a section is open (the default), and a way to change it. */
export function useSummarySectionOpen(name: string): [boolean, (open: boolean) => void] {
  // The app is never server-rendered, so the server snapshot is the same read
  // (which also lets a string render show a folded section).
  const isOpen = () => !read().has(name);
  const open = React.useSyncExternalStore(subscribe, isOpen, isOpen);
  const set = React.useCallback((value: boolean) => setSummarySectionOpen(name, value), [name]);
  return [open, set];
}
