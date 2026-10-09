import { cancelWorkspaceSearch, searchWorkspace } from "@/bridge/workspaceCode";
import { toAppError } from "@/bridge/errors";
import { isCancelError } from "@/bridge/errors";
import { newSearchSession } from "./codegraphStore";
import { set, snapshot } from "./storeAccess";
import { scopeFolderIds, type Scope } from "./workspace/scope";
import { SEARCH_DEBOUNCE_MS, requestSignature, searchable, splitPatterns, type SearchOptions, type SearchSession } from "./workspace/searchModel";

/**
 * Find in workspace: the session in the sidebar's Search section.
 *
 * Go does the reading and matching; this only asks, and keeps the one answer
 * that is still the answer. A request is identified by what it asked
 * (workspace, folders, options); a result whose request no longer matches the
 * form is dropped, so typing faster than the disk never shows hits for a
 * previous query. A superseded request is cancelled on the Go side too.
 */

let timer: ReturnType<typeof setTimeout> | undefined;
let inFlight: { id: string } | null = null;
let seq = 0;

const patchSession = (patch: Partial<SearchSession>) => set({ search: { ...snapshot().search, ...patch } });

/** The folders a session searches, resolved against the workspace as it is now. */
export function sessionFolderIds(s: SearchSession): string[] {
  return scopeFolderIds(s.scope, snapshot().folders);
}

/**
 * Starts a fresh session. Its scope is copied from where it was asked from — the
 * sidebar's scope, or one folder's context menu — and is its own from then on:
 * the header's picker changing afterwards does not change what this searches.
 */
export function openSearch(scope: Scope, query?: string): void {
  cancelInFlight();
  const { workspaceId, search } = snapshot();
  const fresh = newSearchSession(workspaceId, scope);
  set({
    activeTab: "search",
    search: { ...fresh, options: { ...fresh.options, ...(query !== undefined ? { query } : { query: search.options.query }) } },
  });
  runSoon(0);
}

export function setSearchOptions(patch: Partial<SearchOptions>): void {
  patchSession({ options: { ...snapshot().search.options, ...patch } });
  // The search running now answers a question that was just changed: stop it at
  // once instead of letting it read files through the debounce for nothing. Its
  // answer is dropped either way (its request is no longer the one in flight).
  if (inFlight) {
    stopOnTheGoSide(inFlight.id);
    inFlight = null;
  }
  runSoon(SEARCH_DEBOUNCE_MS);
}

export function setSearchScope(scope: Scope): void {
  patchSession({ scope });
  runSoon(0);
}

export function toggleSearchGroup(key: string): void {
  const { collapsed } = snapshot().search;
  patchSession({ collapsed: collapsed.includes(key) ? collapsed.filter((k) => k !== key) : [...collapsed, key] });
}

export function collapseSearchGroups(keys: string[]): void {
  patchSession({ collapsed: keys });
}

/**
 * Stops a superseded search where it runs. The call is deliberately not
 * cancelled on this side as well: Wails would then report the "operation
 * cancelled" answer Go sends back as an unhandled rejection of a cancelled
 * promise. Left alone, the call rejects normally and the catch below drops it
 * because its request is no longer the one in flight.
 */
function stopOnTheGoSide(requestId: string) {
  void cancelWorkspaceSearch(requestId).catch(() => undefined);
}

function cancelInFlight() {
  clearTimeout(timer);
  if (inFlight) {
    const { id } = inFlight;
    inFlight = null;
    stopOnTheGoSide(id);
  }
}

function runSoon(delay: number) {
  clearTimeout(timer);
  timer = setTimeout(() => void runSearch(), delay);
}

/** Runs the session's search now, replacing any in flight. */
export async function runSearch(): Promise<void> {
  clearTimeout(timer);
  const { workspaceId, search } = snapshot();
  const o = search.options;
  if (!workspaceId || !searchable(o)) {
    cancelInFlight();
    patchSession({ status: "idle", error: "", result: null, resultFor: "" });
    return;
  }
  const folderIds = sessionFolderIds(search);
  const signature = requestSignature(workspaceId, folderIds, o);
  if (search.status === "done" && search.resultFor === signature) return;

  // One request at a time: the previous one is stopped before the next begins.
  if (inFlight) {
    stopOnTheGoSide(inFlight.id);
  }
  const requestId = `${search.id}:${++seq}`;
  const call = searchWorkspace({
    requestId,
    workspaceId,
    folderIds,
    query: o.query,
    regex: o.regex,
    caseSensitive: o.caseSensitive,
    wholeWord: o.wholeWord,
    include: splitPatterns(o.include),
    exclude: splitPatterns(o.exclude),
  });
  inFlight = { id: requestId };
  patchSession({ status: "searching", error: "" });
  const still = () => inFlight?.id === requestId && snapshot().workspaceId === workspaceId && snapshot().search.id === search.id;
  try {
    const result = await call;
    if (!still()) return;
    inFlight = null;
    // The form moved on while this ran: its answer is not the answer.
    const now = snapshot().search;
    if (requestSignature(workspaceId, sessionFolderIds(now), now.options) !== signature) {
      runSoon(0);
      return;
    }
    patchSession({ status: "done", error: "", result, resultFor: signature });
  } catch (e) {
    if (isCancelError(e) || !still()) return;
    inFlight = null;
    const failure = toAppError(e);
    patchSession({
      status: failure.code === "invalid_query" ? "invalid" : "failed",
      error: failure.message,
      // The previous hits stay: an invalid pattern mid-typing should not blank the list.
    });
  }
}

/** Leaving the workspace or closing the section: nothing may keep reading files. */
export function stopSearch(): void {
  cancelInFlight();
  if (snapshot().search.status === "searching") patchSession({ status: "idle" });
}
