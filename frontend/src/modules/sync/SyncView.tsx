import { useEffect, useState } from "react";
import {
	 authLogout,
	 authStatus,
  syncConflicts,
  syncConnect,
  syncDisconnect,
  syncNow,
  syncStatus,
  type AuthStatus,
  type SyncConflict,
  type SyncReport,
  type SyncStatus,
} from "../../bridge/sync";
import { errorMessage } from "../../bridge/errors";
import { ResolvePanel } from "./ResolvePanel";

const disconnected: SyncStatus = {
  connected: false,
  endpoint: "",
  accountId: "",
  deviceId: "",
  expiresAt: "",
};

// The HTTP transport appends /v1/sync/* itself. Keep only the public gateway
// origin here so a released client never connects directly to the API service.
const productionGatewayEndpoint = "https://api.synx.io.vn";
const signedOut: AuthStatus = { authenticated: false, expiresAt: "" };

export function SyncView() {
  const [authentication, setAuthentication] = useState<AuthStatus>(signedOut);
  const [connection, setConnection] = useState<SyncStatus>(disconnected);
  const [report, setReport] = useState<SyncReport | null>(null);
  const [conflicts, setConflicts] = useState<SyncConflict[]>([]);
  const [resolving, setResolving] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function refreshStatus() {
    setConnection(await syncStatus());
  }

  async function refreshAuthentication() {
    setAuthentication(await authStatus());
  }

  async function refreshConflicts() {
    setConflicts(await syncConflicts());
  }

  useEffect(() => {
    refreshStatus().catch((caught) => setError(errorMessage(caught)));
    refreshAuthentication().catch((caught) => setError(errorMessage(caught)));
    refreshConflicts().catch((caught) => setError(errorMessage(caught)));
  }, []);

  async function onConnect() {
    setBusy(true);
    setError("");
    setReport(null);
    try {
      await syncConnect();
      await refreshStatus();
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
			// Authentication can succeed before the gateway handshake fails. Always
			// refresh here so the UI permits Sign out of that stored session.
			await refreshAuthentication().catch((caught) => setError(errorMessage(caught)));
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

  async function onLogout() {
    setBusy(true);
    setError("");
    try {
      await authLogout();
      setAuthentication(signedOut);
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
            value={productionGatewayEndpoint}
            placeholder={productionGatewayEndpoint}
            readOnly
            disabled={busy || connection.connected}
          />
        </label>
        <button
          type="button"
          onClick={onConnect}
          disabled={busy || connection.connected}
        >
          {authentication.authenticated ? "Connect" : "Sign in and connect"}
        </button>
        <button type="button" onClick={onLogout} disabled={busy}>
          Sign out
        </button>
      </div>

      <p className="sync-notice">
        {authentication.authenticated
          ? `Signed in${authentication.expiresAt ? ` until ${authentication.expiresAt}` : ""}. Tokens refresh automatically.`
          : "Sign in opens Keycloak in your browser. DevKit stores only the rotating refresh token in the OS keychain."}
      </p>

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
          {report.pushFailed > 0 ? (
            <div><dt>Dead-lettered</dt><dd>{report.pushFailed}</dd></div>
          ) : null}
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
              <li key={conflict.recordId}>
                <span className="sync-conflict-type">{conflict.recordType}</span>
                <code>{conflict.recordId}</code>
                {conflict.theirsDeleted ? (
                  <span className="sync-conflict-deleted">deleted remotely</span>
                ) : null}
                <span className="sync-conflict-detected">
                  {conflict.detectedAt || "—"}
                </span>
                <button
                  type="button"
                  className="sync-conflict-resolve"
                  onClick={() => setResolving(conflict.recordId)}
                >
                  Resolve
                </button>
              </li>
            ))}
          </ul>
        )}
        {resolving ? (
          <ResolvePanel
            recordId={resolving}
            onClose={() => setResolving(null)}
            onResolved={() => {
              setResolving(null);
              refreshConflicts().catch((caught) => setError(errorMessage(caught)));
            }}
          />
        ) : null}
      </div>

      {error ? <p role="alert">{error}</p> : null}
    </section>
  );
}
