import { beforeEach, describe, expect, it, mock } from "bun:test";

const real = await import("@/bridge/workspaceCode");

type Pending = { req: { requestId: string; query: string; folderIds: string[] }; resolve: (r: unknown) => void; reject: (e: unknown) => void; cancelled: boolean };
const pending: Pending[] = [];
const cancelled: string[] = [];

const result = (tag: string) => ({
  files: [{ folderId: "a", path: tag, matches: [], truncated: false }],
  folders: [],
  matches: 1,
  filesSearched: 1,
  filesSkipped: 0,
  truncated: "",
  elapsedMs: 1,
});

mock.module("@/bridge/workspaceCode", () => ({
  ...real,
  searchWorkspace: (req: Pending["req"]) => {
    let resolve!: (r: unknown) => void;
    let reject!: (e: unknown) => void;
    const promise = new Promise((res, rej) => {
      resolve = res;
      reject = rej;
    }) as Promise<unknown> & { cancel: () => void };
    const entry: Pending = { req, resolve, reject, cancelled: false };
    promise.cancel = () => {
      entry.cancelled = true;
    };
    pending.push(entry);
    return promise;
  },
  cancelWorkspaceSearch: async (id: string) => {
    cancelled.push(id);
  },
}));

const { useCodeGraphStore, newSearchSession } = await import("./codegraphStore");
const actions = await import("./searchActions");
const { WHOLE_WORKSPACE } = await import("./workspace/scope");

const folder = (id: string) => ({ id, workspace_id: "w", name: id, path: `/ws/${id}`, name_source: "" });
const state = () => useCodeGraphStore.getState();

beforeEach(() => {
  pending.length = 0;
  cancelled.length = 0;
  useCodeGraphStore.setState({
    workspaceId: "w",
    folders: [folder("a"), folder("b")],
    primaryId: "a",
    scope: WHOLE_WORKSPACE,
    search: newSearchSession("w", WHOLE_WORKSPACE),
  } as never);
});

describe("workspace search", () => {
  it("searches the scope it was opened with, not the primary", async () => {
    actions.openSearch({ kind: "folders", ids: ["b"] }, "needle");
    const run = actions.runSearch();
    expect(pending).toHaveLength(1);
    expect(pending[0].req.folderIds).toEqual(["b"]);
    expect(pending[0].req.query).toBe("needle");
    pending[0].resolve(result("b.txt"));
    await run;
  });

  it("cancels the previous request and drops its late answer", async () => {
    actions.openSearch(WHOLE_WORKSPACE, "one");
    const first = actions.runSearch();
    actions.setSearchOptions({ query: "two" });
    const second = actions.runSearch();
    await Promise.resolve();

    expect(pending).toHaveLength(2);
    // Stopped by request id on the Go side; the call itself is left to reject on its own.
    expect(cancelled).toContain(pending[0].req.requestId);
    expect(pending[0].cancelled).toBe(false);

    pending[1].resolve(result("second"));
    await second;
    // The first one answers after being superseded.
    pending[0].resolve(result("first"));
    await first;
    expect(state().search.result?.files[0].path).toBe("second");
    expect(state().search.status).toBe("done");
  });

  it("stops the running search as soon as the query changes, not when the debounce ends", async () => {
    actions.openSearch(WHOLE_WORKSPACE, "one");
    const first = actions.runSearch();
    await Promise.resolve();
    expect(cancelled).toHaveLength(0);
    actions.setSearchOptions({ query: "two" });
    // Nothing new has started (that waits for the debounce), yet the old job is already stopped.
    expect(pending).toHaveLength(1);
    expect(cancelled).toContain(pending[0].req.requestId);
    pending[0].resolve(result("late"));
    await first;
    expect(state().search.result).toBeNull();
  });

  it("shows an invalid pattern as invalid, not as a failure, and keeps earlier hits", async () => {
    actions.openSearch(WHOLE_WORKSPACE, "ok");
    const first = actions.runSearch();
    pending[0].resolve(result("kept"));
    await first;

    actions.setSearchOptions({ query: "(", regex: true });
    const bad = actions.runSearch();
    const { AppError } = await import("@/bridge/errors");
    pending[1].reject(new AppError({ code: "invalid_query", message: "bad pattern", recoverable: true, details: undefined } as never));
    await bad;
    expect(state().search.status).toBe("invalid");
    expect(state().search.result?.files[0].path).toBe("kept");
  });

  it("does not show an answer for another workspace", async () => {
    actions.openSearch(WHOLE_WORKSPACE, "x");
    const run = actions.runSearch();
    useCodeGraphStore.setState({ workspaceId: "other", search: newSearchSession("other", WHOLE_WORKSPACE) } as never);
    pending[0].resolve(result("stale"));
    await run;
    expect(state().search.result).toBeNull();
  });
});
