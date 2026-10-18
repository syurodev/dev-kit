import * as React from "react";

/**
 * Which summary sections the person has opened. Every section starts folded —
 * a workspace of many folders would otherwise open with a screenful of
 * Environment, Changes and Pull requests — and each header carries a one-line
 * summary, so the card still tells the story closed. One list for the docked
 * card and the corner popover (both are mounted), remembered across sessions
 * and restarts: a person who always wants Changes open opens it once. Storage
 * can be unavailable or hold junk, so every read and write is guarded and the
 * sections simply start folded.
 *
 * The key is new: the list it replaces held the sections folded, under a
 * default of open, and read as opened it would open exactly those.
 */
const KEY = "devkit.agenthub.summary.opened";

let opened: ReadonlySet<string> | null = null;
const listeners = new Set<() => void>();

function read(): ReadonlySet<string> {
  if (opened) return opened;
  let names: string[] = [];
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(KEY) ?? "[]");
    if (Array.isArray(parsed)) names = parsed.filter((n): n is string => typeof n === "string");
  } catch {
    // Unavailable or malformed: everything starts folded.
  }
  opened = new Set(names);
  return opened;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Opens or folds one section for everyone listening. */
export function setSummarySectionOpen(name: string, open: boolean): void {
  const next = new Set(read());
  if (open) next.add(name);
  else next.delete(name);
  opened = next;
  try {
    localStorage.setItem(KEY, JSON.stringify([...next]));
  } catch {
    // Not remembered; still changed for this window.
  }
  for (const listener of listeners) listener();
}

/** Forgets what was read, so a test can start from storage's own state. */
export function resetSummaryFold(): void {
  opened = null;
  for (const listener of listeners) listener();
}

/** Whether a section is open (folded by default), and a way to change it. */
export function useSummarySectionOpen(name: string): [boolean, (open: boolean) => void] {
  // The app is never server-rendered, so the server snapshot is the same read
  // (which also lets a string render show a folded section).
  const isOpen = () => read().has(name);
  const open = React.useSyncExternalStore(subscribe, isOpen, isOpen);
  const set = React.useCallback((value: boolean) => setSummarySectionOpen(name, value), [name]);
  return [open, set];
}
