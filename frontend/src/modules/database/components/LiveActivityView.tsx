import { useEffect, useState } from "react";
import {
  Activity,
  RefreshCw,
  AlertTriangle,
  ShieldAlert,
  Search,
  CheckCircle2,
  Lock,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useDatabaseStore } from "../databaseStore";
import {
  listActivity,
  cancelQuery,
  terminateSession,
  type SessionRow,
} from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";

export function LiveActivityView() {
  const { profiles, selectedProfileId } = useDatabaseStore();
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [statusMessage, setStatusMessage] = useState("");
  const [search, setSearch] = useState("");

  // Confirmation dialog state (Screen 12)
  // An engine with no server has no sessions to report. That is a different
  // answer from "nothing is running", and the controls below are removed rather
  // than left pointing at data that can never arrive.
  const [engineHasNoSessions, setEngineHasNoSessions] = useState(false);
  const [confirmAction, setConfirmAction] = useState<{
    type: "cancel" | "terminate";
    session: SessionRow;
  } | null>(null);
  const [actionInProgress, setActionInProgress] = useState(false);

  const profile = profiles.find((p) => p.id === selectedProfileId);
  const isProd = profile?.env?.toLowerCase() === "prod" || profile?.env?.toLowerCase() === "production";

  async function fetchSessions() {
    if (!selectedProfileId) return;
    setLoading(true);
    setError("");
    setEngineHasNoSessions(false);
    try {
      const list = await listActivity(selectedProfileId);
      // Sort by duration descending
      const sorted = [...list].sort((a, b) => {
        return (b.duration || "").localeCompare(a.duration || "");
      });
      setSessions(sorted);
    } catch (e) {
      const message = errorMessage(e);
      if (message.includes("does not report sessions")) {
        setEngineHasNoSessions(true);
        setError("");
      } else {
        setError(message);
      }
      setSessions([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void fetchSessions();
  }, [selectedProfileId]);

  async function handleConfirmAction() {
    if (!confirmAction || !selectedProfileId) return;
    setActionInProgress(true);
    setStatusMessage("");
    setError("");

    try {
      if (confirmAction.type === "cancel") {
        await cancelQuery(selectedProfileId, confirmAction.session.pid);
        setStatusMessage(`Query cancelled for PID ${confirmAction.session.pid}`);
      } else {
        await terminateSession(selectedProfileId, confirmAction.session.pid);
        setStatusMessage(`Session terminated for PID ${confirmAction.session.pid}`);
      }
      setConfirmAction(null);
      await fetchSessions();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setActionInProgress(false);
    }
  }

  const filtered = sessions.filter(
    (s) =>
      !search ||
      s.pid.includes(search) ||
      s.user.toLowerCase().includes(search.toLowerCase()) ||
      s.query.toLowerCase().includes(search.toLowerCase()) ||
      s.application.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="flex-1 flex flex-col h-full overflow-hidden select-none">
      {/* Header Toolbar */}
      <div className="px-4 py-3 border-b border-border bg-surface-raised/40 flex flex-wrap items-center justify-between gap-3 shrink-0">
        <div className="flex items-center gap-2">
          <Activity className="w-4 h-4 text-action-primary" />
          <h2 className="text-sm font-semibold text-text-primary">Live Activity Snapshot</h2>
          {!engineHasNoSessions && (
            <span className="text-xs text-text-tertiary">
              ({sessions.length} active sessions)
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
              className="h-7 pl-7 text-xs bg-surface-base w-48"
            />
          </div>

          <Button
            variant="outline"
            size="sm"
            className="h-7 text-xs gap-1.5"
            onClick={fetchSessions}
            disabled={loading || !selectedProfileId}
          >
            <RefreshCw className={`w-3 h-3 ${loading ? "animate-spin" : ""}`} />
            <span>Refresh Snapshot</span>
          </Button>
        </div>
      </div>

      {/* Status or Error Notice */}
      {statusMessage && (
        <div className="bg-status-success/10 border-b border-status-success/30 px-4 py-2 text-xs text-status-success flex items-center justify-between">
          <span className="flex items-center gap-1.5 font-medium">
            <CheckCircle2 className="w-3.5 h-3.5" />
            {statusMessage}
          </span>
        </div>
      )}

      {error && (
        <div className="bg-status-danger/10 border-b border-status-danger/30 px-4 py-2 text-xs text-status-danger flex items-center justify-between">
          <span>{error}</span>
        </div>
      )}

      {/* Main Table / Empty State */}
      <div className="flex-1 overflow-auto">
        {engineHasNoSessions ? (
          <div className="h-full flex flex-col items-center justify-center text-center p-8 text-text-tertiary">
            <div className="w-12 h-12 rounded-xl bg-surface-raised border border-border flex items-center justify-center mb-3">
              <Activity className="w-6 h-6 text-text-secondary" />
            </div>
            <h3 className="text-sm font-semibold text-text-primary mb-1">
              {profile?.engine === "sqlite" ? "SQLite does not report sessions" : "This engine does not report sessions"}
            </h3>
            <p className="text-xs max-w-sm text-text-secondary leading-relaxed">
              There is no server and no connection pool, so there is nothing to inspect.
              Activity is available on PostgreSQL, MySQL and MariaDB profiles.
            </p>
          </div>
        ) : filtered.length > 0 ? (
          <table className="w-full text-left text-xs border-collapse font-mono" aria-label="Live sessions">
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
              {filtered.map((s) => {
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
                      <span className="px-1.5 py-0.5 rounded text-[10px] bg-surface-raised text-text-secondary">
                        {s.state}
                      </span>
                    </td>
                    <td className="px-3 py-2.5 font-medium text-text-primary">
                      {s.duration || "<1s"}
                    </td>
                    <td className="px-3 py-2.5 text-text-secondary">
                      <div className="truncate">{s.user}</div>
                      <div className="text-[10px] text-text-tertiary truncate font-mono">
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
                          <span className="text-[11px] font-semibold text-status-danger">
                            PID {s.blockedBy.join(", ")}
                          </span>
                        </div>
                      ) : (
                        <span className="text-text-tertiary">—</span>
                      )}
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[11px] text-text-secondary truncate max-w-xs" title={s.query}>
                      {s.query || "<idle>"}
                    </td>
                    <td className="px-3 py-2.5 text-right space-x-1">
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-6 px-1.5 text-[10px] text-text-secondary hover:text-text-primary"
                        onClick={() => setConfirmAction({ type: "cancel", session: s })}
                        title="Cancel this running query statement"
                      >
                        Cancel
                      </Button>
                      <Button
                        variant="destructive"
                        size="sm"
                        className="h-6 px-1.5 text-[10px]"
                        onClick={() => setConfirmAction({ type: "terminate", session: s })}
                        title="Terminate connection and rollback transaction"
                      >
                        Terminate
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        ) : (
          <div className="h-full flex flex-col items-center justify-center text-center p-8 text-text-tertiary">
            <div className="w-12 h-12 rounded-xl bg-surface-raised border border-border flex items-center justify-center mb-3">
              <Activity className="w-6 h-6 text-text-secondary" />
            </div>
            <h3 className="text-sm font-semibold text-text-primary mb-1">
              No Active Queries Running
            </h3>
            <p className="text-xs max-w-sm text-text-secondary leading-relaxed">
              This server currently has no long-running or blocked queries. Activity snapshots are taken on-demand when you refresh.
            </p>
          </div>
        )}
      </div>

      {/* Destructive Action Confirmation Dialog (Screen 12) */}
      {confirmAction && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4">
          <div className="bg-surface-base border border-border rounded-xl shadow-xl w-full max-w-md p-5 flex flex-col gap-4 animate-in fade-in zoom-in-95 duration-150">
            <div className="flex items-start gap-3">
              <div className="w-10 h-10 rounded-full bg-status-danger/10 border border-status-danger/30 flex items-center justify-center shrink-0 text-status-danger">
                <AlertTriangle className="w-5 h-5" />
              </div>
              <div className="space-y-1">
                <h3 className="text-sm font-semibold text-text-primary">
                  {confirmAction.type === "cancel"
                    ? "Cancel Query Statement?"
                    : "Terminate Session Connection?"}
                </h3>
                <p className="text-xs text-text-secondary leading-relaxed">
                  {confirmAction.type === "cancel"
                    ? `This will cancel the currently executing query on PID ${confirmAction.session.pid}. The client connection remains open.`
                    : `This will forcefully disconnect session PID ${confirmAction.session.pid} and roll back its transaction.`}
                </p>
              </div>
            </div>

            {/* Target Statement Detail */}
            <div className="p-2.5 rounded-lg bg-surface-raised border border-border text-xs font-mono space-y-1">
              <div className="flex justify-between text-[11px] text-text-tertiary">
                <span>PID: {confirmAction.session.pid}</span>
                <span>User: {confirmAction.session.user}</span>
              </div>
              <div className="text-[11px] text-text-secondary truncate">
                {confirmAction.session.query}
              </div>
            </div>

            {/* Warning notices */}
            <div className="space-y-2">
              {isProd && (
                <div className="p-2 rounded bg-status-danger/10 border border-status-danger/30 text-[11px] text-status-danger flex items-center gap-1.5 font-medium">
                  <ShieldAlert className="w-4 h-4 shrink-0" />
                  <span>Production Environment: This action affects live workloads.</span>
                </div>
              )}

              <p className="text-[11px] text-text-tertiary italic">
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
