import {
  useSavedQueryDeletion,
  confirmSavedQueryDeletion,
} from "../savedQueryDeletion";
import { useState, useDeferredValue } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bookmark,
  History,
  Copy,
  RefreshCw,
  Search,
  Terminal,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/empty-state";
import { FeedbackBanner } from "@/components/ui/feedback";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ListSkeleton } from "@/components/ui/skeleton";
import { useVirtualRows } from "@/components/ui/virtual-rows";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { CodeBlock } from "@/components/code/CodeBlock";
import { listSavedQueries, listQueryHistory } from "@/bridge/database";
import { copyText } from "@/bridge/clipboard";
import { errorMessage } from "@/bridge/errors";
import { notify } from "@/lib/notify";
import { useDatabaseStore } from "../databaseStore";
import {
  openSavedQuery,
  openNewConsoleTab,
  openHistoryQuery,
} from "../documents";
import { useDatabaseChrome } from "../useDatabaseChrome";
import { useIsActiveDocument } from "../useIsActiveDocument";
import { VaultLockedState } from "./DatabaseStates";
import { isVaultLockedError } from "../errorStates";
import {
  libraryPreview,
  filterLibrary,
  type LibraryEntry,
} from "../queryLibrary";

export function QueryLibraryView({ kind }: { kind: "history" | "saved" }) {
  useDatabaseChrome();
  const profileId = useDatabaseStore((s) => s.selectedProfileId);
  if (!profileId)
    return (
      <EmptyState
        icon={kind === "history" ? History : Bookmark}
        title="Choose a connection"
        body="Select a connection in the sidebar to load its queries."
      />
    );
  // Scoped local UI cannot carry preview/delete/search into a different connection.
  return (
    <Library key={`${kind}:${profileId}`} kind={kind} profileId={profileId} />
  );
}
function Library({
  kind,
  profileId,
}: {
  kind: "history" | "saved";
  profileId: string;
}) {
  const active = useIsActiveDocument(`database.${kind}`);
  const visible = useModuleVisibility();
  const client = useQueryClient();
  const [search, setSearch] = useState("");
  const [preview, setPreview] = useState<LibraryEntry | null>(null);
  const intent = useSavedQueryDeletion((s) => s.intent);
  const deleting = intent?.profileId === profileId ? intent.entry : null;
  const pending = !!intent?.pending;
  const deleteError = intent?.profileId === profileId ? intent.error : "";
  const query = useQuery<LibraryEntry[]>({
    queryKey: ["database", kind, profileId],
    queryFn: () =>
      profileId
        ? kind === "saved"
          ? listSavedQueries(profileId)
          : listQueryHistory(profileId)
        : Promise.resolve([]),
    enabled: !!profileId && active,
    staleTime: 0,
    // Cache stays on a same-target refresh; no cross-profile placeholder data.
  });
  const entries = query.data as LibraryEntry[] | undefined;
  const deferredSearch = useDeferredValue(search);
  const filtered = filterLibrary(entries ?? [], deferredSearch);
  const rows = useVirtualRows(filtered.length, 88);
  const error = query.error ? errorMessage(query.error) : "";
  const open = (entry: LibraryEntry) => {
    if (kind === "saved")
      openSavedQuery({ ...entry, name: entry.name ?? "Saved query" });
    else {
      openHistoryQuery(entry);
    }
  };
  const copy = async (entry: LibraryEntry) => {
    try {
      await copyText(entry.sql);
      notify({ id: `db-query-copy:${entry.id}`, title: "SQL copied" });
    } catch (err) {
      notify({
        id: `db-query-copy:${entry.id}`,
        title: errorMessage(err),
        tone: "error",
      });
    }
  };
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden">
      <div className="flex min-h-12 flex-wrap items-center gap-2 border-b border-border p-2">
        <span className="text-body-12 text-text-secondary">
          {kind === "history" ? "Last 50 · this device" : "Encrypted · synced"}
        </span>
        <Badge variant="secondary" className="min-w-12 tabular-nums">
          {entries ? filtered.length : "…"}
          {search ? ` / ${entries?.length ?? 0}` : ""}
        </Badge>
        <div className="flex-1" />
        <div className="relative min-w-0">
          <Search className="pointer-events-none absolute left-2 top-2 size-3.5 text-text-tertiary" />
          <Input
            className="h-8 w-52 max-w-full pl-7"
            value={search}
            aria-label={`Filter ${kind}`}
            placeholder={`Filter ${kind}…`}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
        {search ? (
          <Button size="sm" variant="ghost" onClick={() => setSearch("")}>
            Clear
          </Button>
        ) : null}
        <Button
          size="sm"
          variant="outline"
          disabled={query.isFetching || !profileId}
          onClick={() => void query.refetch()}
        >
          <RefreshCw
            className={`size-3.5 ${query.isFetching ? "animate-spin motion-reduce:animate-none" : ""}`}
          />
          Refresh
        </Button>
        {kind === "saved" ? (
          <Button size="sm" onClick={openNewConsoleTab}>
            New SQL console
          </Button>
        ) : null}
      </div>
      {error ? (
        <FeedbackBanner
          tone="error"
          title="Could not refresh queries"
          action={
            <Button
              size="sm"
              variant="outline"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              Retry
            </Button>
          }
        >
          {error}
        </FeedbackBanner>
      ) : null}
      <div
        ref={rows.scrollRef}
        className="min-h-0 flex-1 overflow-auto scrollbar-gutter-stable p-3"
      >
        {isVaultLockedError(error) ? (
          <VaultLockedState />
        ) : !entries && !error ? (
          <ListSkeleton />
        ) : !filtered.length ? (
          <EmptyState
            icon={kind === "history" ? History : Bookmark}
            title={
              search
                ? "No matching queries"
                : kind === "history"
                  ? "No queries recorded in history yet"
                  : "No saved queries yet"
            }
            body={
              search
                ? "Try a different name or SQL fragment, or clear the filter."
                : kind === "history"
                  ? "Run SQL in a console to record it here."
                  : "Open a console and save a reusable statement with a name."
            }
          />
        ) : (
          <div role="list" aria-label={`${kind} queries`}>
            {rows.padTop ? (
              <div aria-hidden style={{ height: rows.padTop }} />
            ) : null}
            {rows.indices.map((index) => {
              const entry = filtered[index];
              return (
                <div
                  role="listitem"
                  key={entry.id}
                  className="flex h-22 flex-col gap-1 border-b border-border-subtle py-2"
                >
                  <div className="flex min-w-0 items-center gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="min-w-0 flex-1 justify-start truncate"
                      onClick={() => setPreview(entry)}
                    >
                      {entry.name ??
                        (entry.createdAt
                          ? new Date(entry.createdAt).toLocaleString()
                          : "Recent query")}
                    </Button>
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={`Copy SQL ${entry.name ?? entry.id}`}
                      onClick={() => void copy(entry)}
                    >
                      <Copy />
                    </Button>
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={`Open in console ${entry.name ?? entry.id}`}
                      onClick={() => open(entry)}
                    >
                      <Terminal />
                    </Button>
                    {kind === "saved" ? (
                      <Button
                        size="icon-sm"
                        variant="ghost"
                        aria-label={`Delete ${entry.name ?? entry.id}`}
                        disabled={pending}
                        onClick={() => {
                          useSavedQueryDeletion
                            .getState()
                            .open(profileId, entry);
                        }}
                      >
                        <Trash2 />
                      </Button>
                    ) : null}
                  </div>
                  <button
                    type="button"
                    className="truncate text-left font-mono text-code-12 text-text-secondary"
                    onClick={() => setPreview(entry)}
                  >
                    {libraryPreview(entry.sql)}
                  </button>
                </div>
              );
            })}
            {rows.padBottom ? (
              <div aria-hidden style={{ height: rows.padBottom }} />
            ) : null}
          </div>
        )}
      </div>
      <Dialog
        open={!!preview && active && visible}
        onOpenChange={(v) => {
          if (!v && active) setPreview(null);
        }}
      >
        <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{preview?.name ?? "Query preview"}</DialogTitle>
            <DialogDescription>
              Opening a console does not execute this statement.
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-80 overflow-auto rounded-md border border-border bg-surface-raised p-3">
            <CodeBlock
              code={(preview?.sql ?? "").slice(0, 12000)}
              language="sql"
              className="whitespace-pre-wrap break-words text-code-12"
            />
          </div>
          {preview && preview.sql.length > 12000 ? (
            <p className="text-body-12 text-text-secondary">
              Preview shortened. Copy or open the console for the full SQL.
            </p>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              variant="outline"
              onClick={() => preview && void copy(preview)}
            >
              Copy full SQL
            </Button>
            <Button
              onClick={() => {
                if (preview) open(preview);
                setPreview(null);
              }}
            >
              Open in console
            </Button>
          </div>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={!!deleting && active}
        onClose={() => useSavedQueryDeletion.getState().close()}
        title={`Delete ${deleting?.name ?? "saved query"}?`}
        confirmLabel="Delete query"
        pendingLabel="Deleting query…"
        pending={pending}
        error={deleteError}
        onConfirm={() => {
          void confirmSavedQueryDeletion((id) => {
            void client.invalidateQueries({
              queryKey: ["database", "saved", id],
            });
          });
        }}
      >
        This removes the encrypted saved query and syncs the deletion. The open
        console stays available.
      </ConfirmDialog>
    </div>
  );
}
