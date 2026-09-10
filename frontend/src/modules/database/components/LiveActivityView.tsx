import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  RefreshCw,
  AlertTriangle,
  ShieldAlert,
  Search,
  CheckCircle2,
  Lock,
} from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import { useVirtualRows } from "@/components/ui/virtual-rows";

const SESSION_ROW_HEIGHT = 42;
import { Button } from "@/components/ui/button";
import { ListSkeleton } from "@/components/ui/skeleton";
import { Input } from "@/components/ui/input";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import { useIsActiveDocument } from "../useIsActiveDocument";
import {
  listActivity,
  cancelQuery,
  terminateSession,
  type SessionRow,
} from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";
import { useDatabaseChrome } from "../useDatabaseChrome";

export function LiveActivityView() {
  useDatabaseChrome();
  const queryClient = useQueryClient();
  const { profiles, selectedProfileId } = useDatabaseStore(
    useShallow((s) => ({
      profiles: s.profiles,
      selectedProfileId: s.selectedProfileId,
    }))
  );
  // Being mounted is not permission to query a production server. A module's
  // inactive tabs stay mounted so their state survives a switch, so what used
  // to be `activeTab === "activity"` has to become "is my tab the one on
  // screen" now that this view is a document.
  const isActive = useIsActiveDocument("database.activity");

  const [statusMessage, setStatusMessage] = useState("");
  const [mutationError, setMutationError] = useState("");
  const [search, setSearch] = useState("");

  const [confirmAction, setConfirmAction] = useState<{
    type: "cancel" | "terminate";
    session: SessionRow;
  } | null>(null);

  const profile = profiles.find((p) => p.id === selectedProfileId);
  const isProd = profile?.env?.toLowerCase() === "prod" || profile?.env?.toLowerCase() === "production";

  const {
    data: sessionsData,
    isPending,
    isFetching,
    error: queryError,
    refetch,
  } = useQuery({
    queryKey: ["database", "activity", selectedProfileId],
    queryFn: async () => {
      if (!selectedProfileId) return [];
      const list = await listActivity(selectedProfileId);
      return [...list].sort((a, b) => {
        return (b.duration || "").localeCompare(a.duration || "");
      });
    },
    enabled: Boolean(selectedProfileId) && isActive,
    placeholderData: (previousData) => previousData,
    // A snapshot is on-demand by its own copy: re-take it on every entry.
    staleTime: 0,
  });

  const queryErrStr = queryError ? errorMessage(queryError) : "";
  const engineHasNoSessions = queryErrStr.includes("does not report sessions");
  const error = mutationError || (engineHasNoSessions ? "" : queryErrStr);

  const cancelMutation = useMutation({
    mutationFn: async (pid: string) => {
      if (!selectedProfileId) return;
      await cancelQuery(selectedProfileId, pid);
    },
    onSuccess: (_, pid) => {
      setStatusMessage(`Query cancelled for PID ${pid}`);
      void queryClient.invalidateQueries({ queryKey: ["database", "activity", selectedProfileId] });
      setConfirmAction(null);
    },
    onError: (e) => {
      setMutationError(errorMessage(e));
    },
  });

  const terminateMutation = useMutation({
    mutationFn: async (pid: string) => {
      if (!selectedProfileId) return;
      await terminateSession(selectedProfileId, pid);
    },
    onSuccess: (_, pid) => {
      setStatusMessage(`Session terminated for PID ${pid}`);
      void queryClient.invalidateQueries({ queryKey: ["database", "activity", selectedProfileId] });
      setConfirmAction(null);
    },
    onError: (e) => {
      setMutationError(errorMessage(e));
    },
  });

  const actionInProgress = cancelMutation.isPending || terminateMutation.isPending;
  const loading = isFetching;

  async function handleConfirmAction() {
    if (!confirmAction || !selectedProfileId) return;
    setStatusMessage("");
    setMutationError("");
    if (confirmAction.type === "cancel") {
      await cancelMutation.mutateAsync(confirmAction.session.pid);
    } else {
      await terminateMutation.mutateAsync(confirmAction.session.pid);
    }
  }

  const sessions = engineHasNoSessions
    ? []
    : isPending && !sessionsData
      ? null
      : (sessionsData ?? []);

  const filtered = (sessions ?? []).filter(
    (s) =>
      !search ||
      s.pid.includes(search) ||
      s.user.toLowerCase().includes(search.toLowerCase()) ||
      s.query.toLowerCase().includes(search.toLowerCase()) ||
      s.application.toLowerCase().includes(search.toLowerCase())
  );

  // A busy server can report hundreds of sessions; past 100 the table windows
  // (frontend performance 6.3). Rows are py-2.5 around a 17px line box, so 42.
  const sessionRows = useVirtualRows(filtered.length, SESSION_ROW_HEIGHT);

  return (
    <div className="flex-1 flex flex-col h-full overflow-hidden select-none">
      {/* Header Toolbar */}
      {/* No heading: the tab strip directly above already names this view, and
          the same bar height across tabs stops the content jumping on switch. */}
      <div className="min-h-12 py-2 border-b border-border bg-surface-raised/30 flex flex-wrap items-center justify-between gap-3 shrink-0">
        <div className="flex items-center gap-2">
          {!engineHasNoSessions && (
            <span className="text-body-12 text-text-secondary">
              {sessions?.length ?? 0} active {sessions?.length === 1 ? "session" : "sessions"}
            </span>
          )}
        </div>

        <div className={`flex items-center gap-2 ${engineHasNoSessions ? "hidden" : ""}`}>
          <div className="relative">
            <Search className="w-3 h-3 absolute left-2.5 top-1/2 -translate-y-1/2 text-text-tertiary" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search active sessions…"
              className="h-7 pl-7 text-body-12 bg-surface-base w-48"
            />
          </div>

          <Button
            variant="outline"
            size="sm"
            className="h-7 text-label-12 gap-1.5"
            onClick={() => void refetch()}
            disabled={loading || !selectedProfileId}
          >
            <RefreshCw className={`w-3 h-3 ${loading ? "animate-spin" : ""}`} />
            <span>Refresh Snapshot</span>
          </Button>
        </div>
      </div>

      {/* Status or Error Notice */}
      {statusMessage && (
        <div className="bg-status-success/10 border-b border-status-success/30 px-4 py-2 text-body-12 text-status-success flex items-center justify-between">
          <span className="flex items-center gap-1.5 font-medium">
            <CheckCircle2 className="w-3.5 h-3.5" />
            {statusMessage}
          </span>
        </div>
      )}

      {error && (
        <div className="bg-status-danger/10 border-b border-status-danger/30 px-4 py-2 text-body-12 text-status-danger flex items-center justify-between">
          <span>{error}</span>
        </div>
      )}

      {/* Main Table / Empty State */}
      <div ref={sessionRows.scrollRef} className="flex-1 overflow-auto scrollbar-gutter-stable">
        {engineHasNoSessions ? (
          <EmptyState
            icon={Activity}
            title={profile?.engine === "sqlite" ? "SQLite does not report sessions" : "This engine does not report sessions"}
            body="There is no server and no connection pool, so there is nothing to inspect. Activity is available on PostgreSQL, MySQL and MariaDB profiles."
          />
        ) : sessions === null ? (
          <ListSkeleton />
        ) : filtered.length > 0 ? (
          <table className="w-full text-left text-code-12 border-collapse" aria-label="Live sessions">
            <thead className="bg-surface-raised sticky top-0 border-b border-border text-text-secondary z-10">
              <tr>
                <th className="px-3 py-2 font-semibold w-20">PID</th>
                <th className="px-3 py-2 font-semibold w-24">State</th>
                <th className="px-3 py-2 font-semibold w-24">Duration</th>
                <th className="px-3 py-2 font-semibold w-28">User / Client</th>
                <th className="px-3 py-2 font-semibold w-28">Wait Event</th>
                <th className="px-3 py-2 font-semibold w-28">Blocked By</th>
                <th className="px-3 py-2 font-semibold">Query</th>
                <th className="px-3 py-2 font-semibold text-right w-36">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/40 text-text-primary">
              {/* Spacer rows, not absolute positioning: see virtual-rows. */}
              {sessionRows.padTop > 0 && <tr style={{ height: sessionRows.padTop }} aria-hidden />}
              {sessionRows.indices.map((i) => {
                const s = filtered[i];
                const hasBlockers = s.blockedBy && s.blockedBy.length > 0;
                return (
                  <tr
                    key={s.pid}
                    className={`hover:bg-surface-raised/60 transition-colors ${
                      hasBlockers ? "bg-amber-500/5" : ""
                    }`}
                  >
                    <td className="px-3 py-2.5 font-semibold text-action-primary">
                      {s.pid}
                    </td>
                    <td className="px-3 py-2.5">
                      <span className="px-1.5 py-0.5 rounded text-caption-11 bg-surface-raised text-text-secondary">
                        {s.state}
                      </span>
                    </td>
                    <td className="px-3 py-2.5 font-medium text-text-primary">
                      {s.duration || "<1s"}
                    </td>
                    <td className="px-3 py-2.5 text-text-secondary">
                      <div className="truncate">{s.user}</div>
                      <div className="text-caption-11 text-text-tertiary truncate font-mono">
                        {s.client}
                      </div>
                    </td>
                    <td className="px-3 py-2.5 text-text-tertiary">
                      {s.waitEvent || "—"}
                    </td>
                    <td className="px-3 py-2.5">
                      {hasBlockers ? (
                        <div className="flex items-center gap-1">
                          <Lock className="w-3 h-3 text-status-danger shrink-0" />
                          <span className="text-title-11 text-status-danger">
                            PID {s.blockedBy.join(", ")}
                          </span>
                        </div>
                      ) : (
                        <span className="text-text-tertiary">—</span>
                      )}
                    </td>
                    <td className="px-3 py-2.5 text-code-11 text-text-secondary truncate max-w-xs" title={s.query}>
                      {s.query || "<idle>"}
                    </td>
                    <td className="px-3 py-2.5 text-right space-x-1">
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-6 px-1.5 text-caption-11 text-text-secondary hover:text-text-primary"
                        onClick={() => setConfirmAction({ type: "cancel", session: s })}
                        title="Cancel this running query statement"
                      >
                        Cancel
                      </Button>
                      <Button
                        variant="destructive"
                        size="sm"
                        className="h-6 px-1.5 text-caption-11"
                        onClick={() => setConfirmAction({ type: "terminate", session: s })}
                        title="Terminate connection and rollback transaction"
                      >
                        Terminate
                      </Button>
                    </td>
                  </tr>
                );
              })}
              {sessionRows.padBottom > 0 && (
                <tr style={{ height: sessionRows.padBottom }} aria-hidden />
              )}
            </tbody>
          </table>
        ) : (
          <EmptyState
            icon={Activity}
            title="No active queries running"
            body="This server currently has no long-running or blocked queries. Activity snapshots are taken on-demand when you refresh."
          />
        )}
      </div>

      {/* Destructive Action Confirmation Dialog (Screen 12) */}
      {confirmAction && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4">
          <div className="bg-surface-base border border-border rounded-xl shadow-xl w-full max-w-md p-5 flex flex-col gap-4 animate-in fade-in zoom-in-95 duration-(--motion-base)">
            <div className="flex items-start gap-3">
              <div className="w-10 h-10 rounded-full bg-status-danger/10 border border-status-danger/30 flex items-center justify-center shrink-0 text-status-danger">
                <AlertTriangle className="w-5 h-5" />
              </div>
              <div className="space-y-1">
                <h3 className="text-title-14 text-text-primary">
                  {confirmAction.type === "cancel"
                    ? "Cancel Query Statement?"
                    : "Terminate Session Connection?"}
                </h3>
                <p className="text-body-13 text-text-secondary">
                  {confirmAction.type === "cancel"
                    ? `This will cancel the currently executing query on PID ${confirmAction.session.pid}. The client connection remains open.`
                    : `This will forcefully disconnect session PID ${confirmAction.session.pid} and roll back its transaction.`}
                </p>
              </div>
            </div>

            {/* Target Statement Detail */}
            <div className="p-2.5 rounded-lg bg-surface-raised border border-border text-code-12 space-y-1">
              <div className="flex justify-between text-caption-11 text-text-tertiary">
                <span>PID: {confirmAction.session.pid}</span>
                <span>User: {confirmAction.session.user}</span>
              </div>
              <div className="text-caption-11 text-text-secondary truncate">
                {confirmAction.session.query}
              </div>
            </div>

            {/* Warning notices */}
            <div className="space-y-2">
              {isProd && (
                <div className="p-2 rounded bg-status-danger/10 border border-status-danger/30 text-caption-11-medium text-status-danger flex items-center gap-1.5">
                  <ShieldAlert className="w-4 h-4 shrink-0" />
                  <span>Production Environment: This action affects live workloads.</span>
                </div>
              )}

              <p className="text-caption-11 text-text-tertiary italic">
                * Note: Safe mode does not guard against session termination or query cancellation.
              </p>
            </div>

            {/* Actions */}
            <div className="flex items-center justify-end gap-2 pt-2 border-t border-border">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setConfirmAction(null)}
                disabled={actionInProgress}
              >
                Cancel
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={handleConfirmAction}
                disabled={actionInProgress}
              >
                {actionInProgress ? "Processing…" : `Yes, ${confirmAction.type === "cancel" ? "Cancel Query" : "Terminate"}`}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
