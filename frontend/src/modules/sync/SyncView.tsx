import { useEffect, useState } from "react";
import {
  syncConflicts,
  syncConnect,
  syncDisconnect,
  syncNow,
  syncStatus,
  type SyncConflict,
  type SyncReport,
  type SyncStatus,
} from "../../bridge/sync";
import { errorMessage } from "../../bridge/errors";

const disconnected: SyncStatus = {
  connected: false,
  endpoint: "",
  accountId: "",
  deviceId: "",
  expiresAt: "",
};

export function SyncView() {
  const [endpoint, setEndpoint] = useState("");
  const [accessToken, setAccessToken] = useState("");
  const [connection, setConnection] = useState<SyncStatus>(disconnected);
  const [report, setReport] = useState<SyncReport | null>(null);
  const [conflicts, setConflicts] = useState<SyncConflict[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function refreshStatus() {
    setConnection(await syncStatus());
  }

  async function refreshConflicts() {
    setConflicts(await syncConflicts());
  }

  useEffect(() => {
    refreshStatus().catch((caught) => setError(errorMessage(caught)));
    refreshConflicts().catch((caught) => setError(errorMessage(caught)));
  }, []);

  async function onConnect() {
    setBusy(true);
    setError("");
    setReport(null);
    try {
      await syncConnect(endpoint, accessToken);
      await refreshStatus();
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      // The token is runtime-only and must not remain in React state after the
      // connect attempt, regardless of success or failure.
      setAccessToken("");
      setBusy(false);
    }
  }

  async function onSyncNow() {
    setBusy(true);
    setError("");
    try {
      setReport(await syncNow());
      await refreshStatus();
      await refreshConflicts();
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  }

  async function onDisconnect() {
    setBusy(true);
    setError("");
    try {
      await syncDisconnect();
      setConnection(disconnected);
      setReport(null);
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="sync-view">
      <h2>Sync</h2>
      <p className="sync-notice">
        Local vault changes are queued automatically and encrypted before
        upload. On the first Sync now, this vault is associated with the
        authenticated account shown below.
      </p>

      <div className="sync-connect">
        <label>
          Endpoint
          <input
            type="url"
            value={endpoint}
            onChange={(event) => setEndpoint(event.target.value)}
            placeholder="https://sync.example.com"
            disabled={busy || connection.connected}
          />
        </label>
        <label>
          Access token
          <input
            type="password"
            value={accessToken}
            onChange={(event) => setAccessToken(event.target.value)}
            autoComplete="off"
            disabled={busy || connection.connected}
          />
        </label>
        <button
          type="button"
          onClick={onConnect}
          disabled={busy || connection.connected || !endpoint || !accessToken}
        >
          Connect
        </button>
      </div>

      <dl className="sync-status">
        <div><dt>Status</dt><dd>{connection.connected ? "Connected" : "Disconnected"}</dd></div>
        {connection.connected ? (
          <>
            <div><dt>Endpoint</dt><dd>{connection.endpoint}</dd></div>
            <div><dt>Account</dt><dd>{connection.accountId}</dd></div>
            <div><dt>Device</dt><dd>{connection.deviceId}</dd></div>
            <div><dt>Expires</dt><dd>{connection.expiresAt || "—"}</dd></div>
          </>
        ) : null}
      </dl>

      <div className="sync-actions">
        <button type="button" onClick={onSyncNow} disabled={busy || !connection.connected}>
          Sync now
        </button>
        <button type="button" onClick={onDisconnect} disabled={busy || !connection.connected}>
          Disconnect
        </button>
      </div>

      {report ? (
        <dl className="sync-report">
          <div><dt>Push attempted</dt><dd>{report.pushAttempted}</dd></div>
          <div><dt>Push sent</dt><dd>{report.pushSent}</dd></div>
          <div><dt>Push retried</dt><dd>{report.pushRetried}</dd></div>
          <div><dt>Push conflicts</dt><dd>{report.pushConflicts}</dd></div>
          <div><dt>Pulled</dt><dd>{report.pulled}</dd></div>
          <div><dt>Applied</dt><dd>{report.applied}</dd></div>
        </dl>
      ) : null}
      <div className="sync-conflicts">
        <h3>Conflicts ({conflicts.length})</h3>
        {conflicts.length === 0 ? (
          <p className="sync-conflicts-empty">No conflicts to resolve.</p>
        ) : (
          <ul>
            {conflicts.map((conflict) => (
              <li key={conflict.localChangeKey}>
                <span className="sync-conflict-type">{conflict.recordType}</span>
                <code>{conflict.recordId}</code>
                <span className="sync-conflict-detected">
                  {conflict.detectedAt || "—"}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      {error ? <p role="alert">{error}</p> : null}
    </section>
  );
}
