import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import type { CancellableCall } from "@/bridge/client";
import { errorMessage } from "@/bridge/errors";
import {
  browseMemory,
  createMemory,
  deleteMemory,
  getMemory,
  memoryHistory,
  memoryKinds,
  readMemoryRevision,
  restoreMemory,
  updateMemory,
  type BrowsePage,
  type HistoryPage,
  type MemoryEntry,
  type MemoryInput,
  type MemoryRevision,
} from "@/bridge/workspaceMemory";
import {
  emptyMemory,
  LatestRequest,
  isMemoryConflict,
  memoryBrowseInput,
  memoryDraft,
  nextCursors,
  sameDraft,
  sourceLabel,
  validateMemory,
} from "./WorkspaceMemoryModel";

const selectClass =
  "h-8 rounded-md border border-border bg-surface-raised px-2 text-label-12";
const noPage: BrowsePage = { entries: [], hasMore: false, nextCursor: "" };
export default function WorkspaceMemoryDialog({
  workspace,
  onClose,
  portal = true,
}: {
  workspace: { id: string; name: string };
  onClose: () => void;
  portal?: boolean;
}) {
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [kind, setKind] = useState("");
  const [deleted, setDeleted] = useState(false);
  const [cursors, setCursors] = useState([""]);
  const [page, setPage] = useState<BrowsePage>(noPage);
  const [loading, setLoading] = useState(true);
  const [listError, setListError] = useState("");
  const [refresh, setRefresh] = useState(0);
  const [entry, setEntry] = useState<MemoryEntry | null>(null);
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<MemoryInput>(emptyMemory);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [selectedId, setSelectedId] = useState("");
  const [busy, setBusy] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [history, setHistory] = useState<HistoryPage | null>(null);
  const [historyCursors, setHistoryCursors] = useState([""]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState("");
  const [revision, setRevision] = useState<MemoryRevision | null>(null);
  const [revisionLoading, setRevisionLoading] = useState(false);
  const [revisionError, setRevisionError] = useState("");
  const [revisionNumber, setRevisionNumber] = useState(0);
  const [confirm, setConfirm] = useState<{
    title: string;
    description: string;
    action: () => void;
    destructive?: boolean;
  } | null>(null);
  const requests = useRef({
    list: new LatestRequest(),
    detail: new LatestRequest(),
    history: new LatestRequest(),
    revision: new LatestRequest(),
  }).current;
  const pending = useRef<
    Partial<
      Record<
        "list" | "detail" | "history" | "revision",
        CancellableCall<unknown>
      >
    >
  >({}).current;
  const cancel = (name: keyof typeof pending) => {
    pending[name]?.cancel();
    delete pending[name];
  };
  const dirty =
    (creating || !!entry) &&
    !sameDraft(draft, entry ? memoryDraft(entry) : emptyMemory());
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query), 200);
    return () => clearTimeout(timer);
  }, [query]);
  useEffect(
    () => () => {
      Object.values(requests).forEach((request) => request.invalidate());
      Object.values(pending).forEach((call) => call.cancel());
    },
    [requests],
  );
  useEffect(() => {
    if (query !== debounced) return;
    const current = requests.list.begin();
    setLoading(true);
    setListError("");
    setPage(noPage);
    const call = browseMemory(
      workspace.id,
      memoryBrowseInput(debounced, kind, deleted, cursors[cursors.length - 1]),
    );
    pending.list = call;
    void call
      .then(
        (result) => {
          if (current()) setPage(result);
        },
        (e) => {
          if (current()) setListError(errorMessage(e));
        },
      )
      .finally(() => {
        if (current()) setLoading(false);
      });
    return () => {
      requests.list.invalidate();
      call.cancel();
    };
  }, [
    workspace.id,
    query,
    debounced,
    kind,
    deleted,
    cursors,
    refresh,
    requests,
  ]);
  const guard = (action: () => void) => {
    if (busy) return;
    if (dirty)
      setConfirm({
        title: "Discard unsaved changes?",
        description:
          "Your memory draft has unsaved changes. Discard it to continue.",
        action,
      });
    else action();
  };
  const clearDetail = () => {
    requests.detail.invalidate();
    requests.history.invalidate();
    requests.revision.invalidate();
    cancel("detail");
    cancel("history");
    cancel("revision");
    setEntry(null);
    setCreating(false);
    setSelectedId("");
    setDraft(emptyMemory());
    setDetailLoading(false);
    setDetailError("");
    setConflict(false);
    setHistoryOpen(false);
    setHistory(null);
    setHistoryCursors([""]);
    setRevision(null);
    setRevisionLoading(false);
    setRevisionError("");
  };
  const filters = (action: () => void) =>
    guard(() => {
      clearDetail();
      requests.list.invalidate();
      cancel("list");
      setLoading(true);
      setPage(noPage);
      setCursors([""]);
      action();
    });
  const loadEntry = (id: string) => {
    clearDetail();
    setSelectedId(id);
    setDetailLoading(true);
    const current = requests.detail.begin();
    const call = getMemory(workspace.id, id);
    pending.detail = call;
    void call
      .then(
        (result) => {
          if (current()) {
            setEntry(result);
            setDraft(memoryDraft(result));
          }
        },
        (e) => {
          if (current()) setDetailError(errorMessage(e));
        },
      )
      .finally(() => {
        if (current()) setDetailLoading(false);
      });
  };
  const acceptEntry = (result: MemoryEntry) => {
    setEntry(result);
    setSelectedId(result.id);
    setCreating(false);
    setDraft(memoryDraft(result));
    setConflict(false);
    setDetailError("");
    setHistoryOpen(false);
    setRevision(null);
    requests.history.invalidate();
    requests.revision.invalidate();
    setHistory(null);
    setHistoryCursors([""]);
    setCursors([""]);
    setRefresh((value) => value + 1);
  };
  const mutate = async (operation: () => Promise<MemoryEntry>) => {
    setBusy(true);
    setDetailError("");
    try {
      acceptEntry(await operation());
    } catch (e) {
      setDetailError(errorMessage(e));
      setConflict(isMemoryConflict(e));
    } finally {
      setBusy(false);
    }
  };
  const save = () => {
    const validation = validateMemory(draft);
    if (validation) {
      setDetailError(validation);
      return;
    }
    void mutate(() =>
      entry
        ? updateMemory(workspace.id, entry.id, entry.revision, draft)
        : createMemory(workspace.id, draft),
    );
  };
  useEffect(() => {
    if (!historyOpen || !entry) return;
    const current = requests.history.begin();
    setHistoryLoading(true);
    setHistoryError("");
    setHistory(null);
    const call = memoryHistory(
      workspace.id,
      entry.id,
      historyCursors[historyCursors.length - 1],
    );
    pending.history = call;
    void call
      .then(
        (result) => {
          if (current()) setHistory(result);
        },
        (e) => {
          if (current()) setHistoryError(errorMessage(e));
        },
      )
      .finally(() => {
        if (current()) setHistoryLoading(false);
      });
    return () => {
      requests.history.invalidate();
      call.cancel();
    };
  }, [historyOpen, entry, workspace.id, historyCursors, requests]);
  const readRevision = (number: number) => {
    if (!entry) return;
    cancel("revision");
    setRevisionNumber(number);
    const current = requests.revision.begin();
    setRevisionLoading(true);
    setRevision(null);
    setRevisionError("");
    const call = readMemoryRevision(workspace.id, entry.id, number);
    pending.revision = call;
    void call
      .then(
        (result) => {
          if (current()) setRevision(result);
        },
        (e) => {
          if (current()) setRevisionError(errorMessage(e));
        },
      )
      .finally(() => {
        if (current()) setRevisionLoading(false);
      });
  };
  const confirmDelete = () => {
    if (!entry) return;
    const original = entry;
    setConfirm({
      title: "Delete this memory?",
      description:
        "Agents will stop seeing it. Its history remains available and it can be restored." +
        (dirty
          ? " Your unsaved draft will be discarded if deletion succeeds."
          : ""),
      destructive: true,
      action: () => {
        void mutate(() =>
          deleteMemory(workspace.id, original.id, original.revision),
        );
      },
    });
  };
  const confirmRestore = (target: number) => {
    if (!entry) return;
    const original = entry;
    guard(() =>
      setConfirm({
        title: `Restore revision ${target}?`,
        description:
          "This creates a new active revision from the selected snapshot. The existing history is preserved.",
        action: () => {
          void mutate(() =>
            restoreMemory(workspace.id, original.id, original.revision, target),
          );
        },
      }),
    );
  };
  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) guard(onClose);
        }}
      >
        <DialogContent
          portal={portal}
          className="flex h-[min(780px,90vh)] w-[min(1100px,95vw)] flex-col gap-0 overflow-hidden p-0 sm:max-w-[1100px] [&>[data-slot=dialog-body]]:h-full [&>[data-slot=dialog-body]]:min-h-0 [&>[data-slot=dialog-body]]:flex-1 [&>[data-slot=dialog-body]]:shrink"
          onInteractOutside={(event) => {
            if (dirty || busy) event.preventDefault();
          }}
        >
          <DialogHeader className="shrink-0 border-b border-border p-5 pr-12">
            <DialogTitle>Workspace memory · {workspace.name}</DialogTitle>
            <DialogDescription>
              Shared knowledge for agents in this workspace. Stored locally on
              this device.
            </DialogDescription>
          </DialogHeader>
          <div className="flex min-h-0 flex-1 overflow-hidden">
            <aside className="flex w-80 shrink-0 flex-col border-r border-border p-3">
              <label className="sr-only" htmlFor="workspace-memory-search">
                Search workspace memory
              </label>
              <Input
                id="workspace-memory-search"
                autoFocus
                placeholder="Search memory…"
                value={query}
                onChange={(event) => {
                  const value = event.target.value;
                  filters(() => setQuery(value));
                }}
                disabled={busy}
              />
              <p className="mt-1 text-caption-11 text-text-tertiary">
                Search words in titles, summaries and details.
              </p>
              <div className="my-2 flex gap-2">
                <select
                  aria-label="Memory kind filter"
                  className={`${selectClass} min-w-0 flex-1`}
                  value={kind}
                  disabled={busy}
                  onChange={(event) => {
                    const value = event.target.value;
                    filters(() => setKind(value));
                  }}
                >
                  <option value="">All kinds</option>
                  {memoryKinds.map((value) => (
                    <option key={value}>{value}</option>
                  ))}
                </select>
                <select
                  aria-label="Memory status filter"
                  className={selectClass}
                  value={deleted ? "deleted" : "active"}
                  disabled={busy}
                  onChange={(event) => {
                    const value = event.target.value === "deleted";
                    filters(() => setDeleted(value));
                  }}
                >
                  <option value="active">Active</option>
                  <option value="deleted">Deleted</option>
                </select>
              </div>
              <div className="mb-2 flex gap-2">
                <Button
                  size="sm"
                  disabled={busy}
                  onClick={() =>
                    guard(() => {
                      clearDetail();
                      setCreating(true);
                    })
                  }
                >
                  New memory
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy || loading}
                  onClick={() => {
                    setCursors([""]);
                    setRefresh((value) => value + 1);
                  }}
                >
                  Refresh
                </Button>
              </div>
              <div
                className="min-h-0 flex-1 overflow-y-auto"
                aria-busy={loading}
              >
                {loading ? (
                  <p role="status" className="p-3 text-text-secondary">
                    Loading memory…
                  </p>
                ) : listError ? (
                  <div role="alert" className="p-3">
                    <p>{listError}</p>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setRefresh((value) => value + 1)}
                    >
                      Retry
                    </Button>
                  </div>
                ) : page.entries.length === 0 ? (
                  <p className="p-3 text-text-secondary">
                    No {deleted ? "deleted " : ""}memory matches this view.
                  </p>
                ) : (
                  page.entries.map((item) => (
                    <button
                      key={item.id}
                      type="button"
                      aria-pressed={selectedId === item.id}
                      disabled={busy}
                      onClick={() => guard(() => loadEntry(item.id))}
                      className={`mb-1 w-full rounded-md p-3 text-left hover:bg-accent ${selectedId === item.id ? "bg-accent ring-1 ring-border" : ""}`}
                    >
                      <span className="block truncate font-medium">
                        {item.title}
                      </span>
                      <span className="block text-caption-11 text-text-tertiary">
                        {item.kind} · revision {item.revision}
                      </span>
                      <span className="mt-1 line-clamp-2 text-label-12 text-text-secondary">
                        {item.summary}
                      </span>
                    </button>
                  ))
                )}
              </div>
              <div className="mt-2 flex items-center justify-between border-t border-border pt-2">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={loading || busy || cursors.length === 1}
                  onClick={() =>
                    guard(() => {
                      clearDetail();
                      setCursors((value) => value.slice(0, -1));
                    })
                  }
                >
                  Previous
                </Button>
                <span className="text-caption-11 text-text-tertiary">
                  Page {cursors.length}
                </span>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={loading || busy || !page.hasMore}
                  onClick={() =>
                    guard(() => {
                      clearDetail();
                      setCursors((value) =>
                        nextCursors(value, page.nextCursor),
                      );
                    })
                  }
                >
                  Next
                </Button>
              </div>
            </aside>
            <main className="min-w-0 flex-1 overflow-y-auto p-5">
              {detailLoading ? (
                <p role="status">Loading memory…</p>
              ) : !entry && !creating ? (
                <div className="p-6 text-text-secondary">
                  <p>Select a memory to read or edit it.</p>
                  {detailError && (
                    <div role="alert">
                      <p>{detailError}</p>
                      <Button onClick={() => loadEntry(selectedId)}>
                        Retry
                      </Button>
                    </div>
                  )}
                </div>
              ) : (
                <>
                  {entry && (
                    <div className="mb-4 break-words text-caption-11 text-text-secondary">
                      <p>
                        Revision {entry.revision} ·{" "}
                        {entry.deleted ? "Deleted" : "Active"} ·{" "}
                        {sourceLabel(entry.source)}
                      </p>
                      <p className="mt-1">
                        Updated {new Date(entry.updatedAt).toLocaleString()}
                      </p>
                    </div>
                  )}
                  <form
                    onSubmit={(event) => {
                      event.preventDefault();
                      save();
                    }}
                    className="space-y-3"
                  >
                    <label className="block text-label-12">
                      Kind
                      <select
                        className={`${selectClass} ml-3`}
                        value={draft.kind}
                        disabled={busy || !!entry?.deleted}
                        onChange={(event) =>
                          setDraft((value) => ({
                            ...value,
                            kind: event.target.value,
                          }))
                        }
                      >
                        {memoryKinds.map((value) => (
                          <option key={value}>{value}</option>
                        ))}
                      </select>
                    </label>
                    <label className="block text-label-12">
                      Title
                      <Input
                        className="mt-1"
                        value={draft.title}
                        disabled={busy || !!entry?.deleted}
                        onChange={(event) =>
                          setDraft((value) => ({
                            ...value,
                            title: event.target.value,
                          }))
                        }
                      />
                    </label>
                    <label className="block text-label-12">
                      Summary
                      <Textarea
                        className="mt-1 min-h-20"
                        value={draft.summary}
                        disabled={busy || !!entry?.deleted}
                        onChange={(event) =>
                          setDraft((value) => ({
                            ...value,
                            summary: event.target.value,
                          }))
                        }
                      />
                      <span className="text-caption-11 text-text-tertiary">
                        The summary is shared with agents each turn.
                      </span>
                    </label>
                    <label className="block text-label-12">
                      Details
                      <Textarea
                        className="mt-1 min-h-40 font-mono"
                        value={draft.body}
                        disabled={busy || !!entry?.deleted}
                        onChange={(event) =>
                          setDraft((value) => ({
                            ...value,
                            body: event.target.value,
                          }))
                        }
                      />
                    </label>
                    {detailError && (
                      <p role="alert" className="text-destructive">
                        {detailError}
                      </p>
                    )}
                    {conflict && (
                      <div className="rounded border border-border p-3 text-label-12">
                        <p>
                          This memory changed in another session. Your draft has
                          been kept. Reload the latest revision to continue.
                        </p>
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          disabled={busy}
                          onClick={() => guard(() => loadEntry(entry!.id))}
                        >
                          Reload latest
                        </Button>
                      </div>
                    )}
                    <div className="flex gap-2">
                      {!entry?.deleted && (
                        <Button
                          type="submit"
                          disabled={busy || conflict || (!creating && !dirty)}
                        >
                          {busy ? "Saving…" : "Save memory"}
                        </Button>
                      )}
                      {entry && (
                        <Button
                          type="button"
                          variant="outline"
                          disabled={busy}
                          onClick={() => setHistoryOpen((value) => !value)}
                        >
                          {historyOpen ? "Hide history" : "History"}
                        </Button>
                      )}
                      {entry && !entry.deleted && (
                        <Button
                          type="button"
                          variant="outline"
                          disabled={busy || conflict}
                          onClick={confirmDelete}
                        >
                          Delete…
                        </Button>
                      )}
                      {entry?.deleted && (
                        <Button
                          type="button"
                          disabled={busy || conflict}
                          onClick={() => confirmRestore(entry.revision)}
                        >
                          Restore…
                        </Button>
                      )}
                    </div>
                  </form>
                  {historyOpen && (
                    <section
                      className="mt-6 border-t border-border pt-4"
                      aria-label="Memory revision history"
                    >
                      <h3 className="font-medium">Revision history</h3>
                      {historyLoading ? (
                        <p role="status">Loading history…</p>
                      ) : historyError ? (
                        <div role="alert">
                          <p>{historyError}</p>
                          <Button
                            size="sm"
                            onClick={() =>
                              setHistoryCursors((value) => [...value])
                            }
                          >
                            Retry history
                          </Button>
                        </div>
                      ) : (
                        history?.entries.map((item) => (
                          <button
                            key={item.revision}
                            type="button"
                            disabled={busy}
                            onClick={() => readRevision(item.revision)}
                            className="mt-1 block w-full rounded p-2 text-left hover:bg-accent"
                          >
                            <span className="block text-label-12">
                              Revision {item.revision} · {item.operation} ·{" "}
                              {item.title}
                            </span>
                            <span className="block text-caption-11 text-text-tertiary">
                              {sourceLabel(item.source)} ·{" "}
                              {new Date(item.updatedAt).toLocaleString()}
                            </span>
                          </button>
                        ))
                      )}
                      <div className="mt-2 flex gap-2">
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={
                            busy ||
                            historyLoading ||
                            historyCursors.length === 1
                          }
                          onClick={() => {
                            requests.revision.invalidate();
                            cancel("revision");
                            setRevisionLoading(false);
                            setRevisionError("");
                            setRevision(null);
                            setHistoryCursors((value) => value.slice(0, -1));
                          }}
                        >
                          Previous revisions
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busy || historyLoading || !history?.hasMore}
                          onClick={() => {
                            requests.revision.invalidate();
                            cancel("revision");
                            setRevisionLoading(false);
                            setRevisionError("");
                            setRevision(null);
                            setHistoryCursors((value) =>
                              nextCursors(value, history!.nextCursor),
                            );
                          }}
                        >
                          Older revisions
                        </Button>
                      </div>
                      {revisionLoading && (
                        <p role="status">Loading revision…</p>
                      )}
                      {revisionError && (
                        <div role="alert">
                          <p>{revisionError}</p>
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={busy}
                            onClick={() => readRevision(revisionNumber)}
                          >
                            Retry revision
                          </Button>
                        </div>
                      )}
                      {revision && (
                        <div className="mt-3 rounded border border-border p-3">
                          <p className="font-medium">
                            Revision {revision.entry.revision} ·{" "}
                            {revision.entry.title}
                          </p>
                          <p className="my-2 text-label-12">
                            {revision.entry.summary}
                          </p>
                          <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-words text-label-12">
                            {revision.entry.body}
                          </pre>
                          <Button
                            className="mt-3"
                            size="sm"
                            disabled={busy || conflict}
                            onClick={() =>
                              confirmRestore(revision.entry.revision)
                            }
                          >
                            Restore this revision…
                          </Button>
                        </div>
                      )}
                    </section>
                  )}
                </>
              )}
            </main>
          </div>
        </DialogContent>
      </Dialog>
      <AlertDialog
        open={confirm !== null}
        onOpenChange={(open) => {
          if (!open) setConfirm(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirm?.title}</AlertDialogTitle>
            <AlertDialogDescription>
              {confirm?.description}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const action = confirm?.action;
                setConfirm(null);
                if (action) queueMicrotask(action);
              }}
            >
              {confirm?.destructive ? "Delete memory" : "Continue"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
