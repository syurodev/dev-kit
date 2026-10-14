import * as React from "react";
import { useQueries } from "@tanstack/react-query";
import { queryClient } from "@/lib/query";
import { listWorkspaceServices } from "@/bridge/workspace";
import { searchWorkspaceSymbols, type FolderSymbols } from "@/bridge/workspaceCode";
import { exactSymbolMatch, lastSegment, symbolNames, type SymbolHit } from "./codeRefs";
import type { RefContext } from "./resolveFileRef";

const SYMBOL_STALE_MS = 300_000;
const SYMBOL_PENDING_STALE_MS = 15_000;
/** Symbol searches in flight at once, across every message on screen. */
const SYMBOL_CONCURRENCY = 3;
const FOLDERS_STALE_MS = 30_000;

/** The workspace's folders, shared by every symbol lookup and commit open of the moment. */
export function workspaceFolders(workspaceId: string): Promise<{ id: string; name: string; path: string }[]> {
  return queryClient.fetchQuery({
    queryKey: ["agenthub", "workspaceFolders", workspaceId] as const,
    queryFn: async () => (await listWorkspaceServices(workspaceId)).map((s) => ({ id: s.id, name: s.name, path: s.path })),
    staleTime: FOLDERS_STALE_MS,
  });
}

function abortError(signal: AbortSignal): unknown {
  return signal.reason ?? new DOMException("Aborted", "AbortError");
}

/**
 * Runs at most `max` tasks at once, the rest in arrival order. A task whose
 * signal aborts while it waits leaves the queue and never runs: a message
 * scrolled away or unmounted stops costing anything.
 */
export function createLimiter(max: number) {
  let active = 0;
  const queue: { start: () => void; signal?: AbortSignal }[] = [];
  const next = () => {
    while (active < max && queue.length > 0) {
      const entry = queue.shift()!;
      if (entry.signal?.aborted) continue;
      entry.start();
    }
  };
  return function limit<T>(task: () => Promise<T>, signal?: AbortSignal): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      if (signal?.aborted) {
        reject(abortError(signal));
        return;
      }
      const onAbort = () => {
        const at = queue.indexOf(entry);
        if (at >= 0) queue.splice(at, 1);
        reject(abortError(signal!));
      };
      const entry = {
        signal,
        start: () => {
          signal?.removeEventListener("abort", onAbort);
          active++;
          task()
            .then(resolve, reject)
            .finally(() => {
              active--;
              next();
            });
        },
      };
      signal?.addEventListener("abort", onAbort, { once: true });
      queue.push(entry);
      next();
    });
  };
}

const limitSymbolSearch = createLimiter(SYMBOL_CONCURRENCY);

/**
 * What a lookup found. `pending` is a miss that may not last: some folder's
 * index is not ready (indexing, never indexed, failed), so the answer is kept
 * briefly rather than for five minutes.
 */
export type SymbolLookup = { hit: SymbolHit | null; pending: boolean };

export type SymbolLookupDeps = {
  search: (workspaceId: string, folderIds: string[], query: string) => Promise<FolderSymbols[]>;
  folders: (workspaceId: string) => Promise<{ id: string; name: string }[]>;
};

const liveSymbolDeps: SymbolLookupDeps = { search: searchWorkspaceSymbols, folders: workspaceFolders };

const PENDING_STATES: ReadonlySet<string> = new Set(["indexing", "not-indexed", "failed"]);

/** The one symbol in the workspace's indexes `name` names exactly, if any. Searches run three at a time. */
export async function lookupWorkspaceSymbol(
  workspaceId: string,
  name: string,
  signal?: AbortSignal,
  deps: SymbolLookupDeps = liveSymbolDeps,
): Promise<SymbolLookup> {
  const results = await limitSymbolSearch(() => deps.search(workspaceId, [], lastSegment(name)), signal);
  const matches = results.flatMap((folder) =>
    folder.matches.map((m) => ({ folderId: folder.folderId, filePath: m.filePath, line: m.line, kind: m.kind, label: m.label })),
  );
  const hit = exactSymbolMatch(name, matches);
  if (!hit) return { hit: null, pending: results.some((folder) => PENDING_STATES.has(folder.state)) };
  // The folder's name is only for the preview's header; a folder list that fails costs it nothing else.
  const folders = await deps.folders(workspaceId).catch(() => []);
  const folderName = folders.find((f) => f.id === hit.folderId)?.name ?? "";
  return { hit: { ...hit, workspaceId, folderName }, pending: false };
}

/** How long a lookup stands: five minutes for an answer, fifteen seconds while an index is not ready. */
export function symbolStaleTime(data: SymbolLookup | undefined): number {
  return data?.pending ? SYMBOL_PENDING_STALE_MS : SYMBOL_STALE_MS;
}

const NONE: ReadonlyMap<string, SymbolHit> = new Map();

/**
 * The code spans of one finished message that name a symbol, by span text.
 * Nothing is looked up while the message is live or outside a workspace; at
 * most 12 names, each cached by workspace and name (see symbolStaleTime). `lookup`
 * replaces the query (tests, the dev gallery).
 */
export function useCodeRefs(
  spans: readonly string[],
  ctx: RefContext | null,
  settled: boolean,
  lookup?: (name: string) => SymbolHit | null | undefined,
): ReadonlyMap<string, SymbolHit> {
  const workspaceId = ctx?.workspaceId ?? "";
  const names = React.useMemo(() => (settled && workspaceId ? symbolNames(spans) : []), [settled, workspaceId, spans]);
  const queried = React.useMemo(() => (lookup ? [] : names), [names, lookup]);
  const combine = React.useCallback(
    (results: { data: SymbolLookup | undefined }[]): ReadonlyMap<string, SymbolHit> => {
      const map = new Map<string, SymbolHit>();
      results.forEach((result, index) => {
        if (result.data?.hit) map.set(queried[index], result.data.hit);
      });
      return map.size > 0 ? map : NONE;
    },
    [queried],
  );
  const fromQueries = useQueries(
    {
      queries: queried.map((name) => ({
        queryKey: ["agenthub", "symbol", workspaceId, name] as const,
        queryFn: ({ signal }: { signal: AbortSignal }) => lookupWorkspaceSymbol(workspaceId, name, signal),
        staleTime: (query: { state: { data: SymbolLookup | undefined } }) => symbolStaleTime(query.state.data),
      })),
      combine,
    },
    queryClient,
  );
  const fromLookup = React.useMemo(() => {
    if (!lookup || names.length === 0) return NONE;
    const map = new Map<string, SymbolHit>();
    for (const name of names) {
      const hit = lookup(name);
      if (hit) map.set(name, hit);
    }
    return map;
  }, [lookup, names]);
  return lookup ? fromLookup : fromQueries;
}
