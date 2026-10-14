import * as React from "react";
import { useQueries } from "@tanstack/react-query";
import { queryClient } from "@/lib/query";
import { listWorkspaceServices } from "@/bridge/workspace";
import { searchWorkspaceSymbols } from "@/bridge/workspaceCode";
import { exactSymbolMatch, lastSegment, symbolNames, type SymbolHit } from "./codeRefs";
import type { RefContext } from "./resolveFileRef";

const SYMBOL_STALE_MS = 300_000;
const FOLDERS_STALE_MS = 30_000;

/** The workspace's folders, shared by every symbol lookup and commit open of the moment. */
export function workspaceFolders(workspaceId: string): Promise<{ id: string; name: string; path: string }[]> {
  return queryClient.fetchQuery({
    queryKey: ["agenthub", "workspaceFolders", workspaceId] as const,
    queryFn: async () => (await listWorkspaceServices(workspaceId)).map((s) => ({ id: s.id, name: s.name, path: s.path })),
    staleTime: FOLDERS_STALE_MS,
  });
}

/** The one symbol in the workspace's indexes `name` names exactly, or null. */
export async function lookupWorkspaceSymbol(workspaceId: string, name: string): Promise<SymbolHit | null> {
  const results = await searchWorkspaceSymbols(workspaceId, [], lastSegment(name));
  const matches = results.flatMap((folder) =>
    folder.matches.map((m) => ({ folderId: folder.folderId, filePath: m.filePath, line: m.line, kind: m.kind, label: m.label })),
  );
  const hit = exactSymbolMatch(name, matches);
  if (!hit) return null;
  // The folder's name is only for the preview's header; a folder list that fails costs it nothing else.
  const folders = await workspaceFolders(workspaceId).catch(() => []);
  const folderName = folders.find((f) => f.id === hit.folderId)?.name ?? "";
  return { ...hit, workspaceId, folderName };
}

const NONE: ReadonlyMap<string, SymbolHit> = new Map();

/**
 * The code spans of one finished message that name a symbol, by span text.
 * Nothing is looked up while the message is live or outside a workspace; at
 * most 20 names, each cached by workspace and name for five minutes. `lookup`
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
    (results: { data: SymbolHit | null | undefined }[]): ReadonlyMap<string, SymbolHit> => {
      const map = new Map<string, SymbolHit>();
      results.forEach((result, index) => {
        if (result.data) map.set(queried[index], result.data);
      });
      return map.size > 0 ? map : NONE;
    },
    [queried],
  );
  const fromQueries = useQueries(
    {
      queries: queried.map((name) => ({
        queryKey: ["agenthub", "symbol", workspaceId, name] as const,
        queryFn: () => lookupWorkspaceSymbol(workspaceId, name),
        staleTime: SYMBOL_STALE_MS,
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
