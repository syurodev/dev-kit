import { useEffect, useState } from "react";
import { ShieldCheck, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { cn } from "@/lib/utils";
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
import { SyncSidebar } from "./SyncSidebar";
import { pullSegments, pushSegments, type MeterSegment } from "./syncMeter";

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

const CARD = "flex flex-col rounded-md border border-border-subtle bg-surface-well";

/** Figma `field/*` (779:110): a caps label over the value. */
function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <span className="flex min-w-0 flex-1 flex-col gap-0.75">
      <span className="text-label-11-caps text-text-tertiary">{label}</span>
      <span className={cn("truncate text-text-primary", mono ? "text-code-12" : "text-body-13")}>{value}</span>
    </span>
  );
}

/** Figma `meter/*` (779:127): head, a 12px segmented track, a dotted legend. */
function Meter({ label, total, segments }: { label: string; total: number; segments: MeterSegment[] }) {
  return (
    <div className="flex flex-col gap-2.5">
      <div className="flex items-center gap-2 text-label-11-caps">
        <span className="flex-1 text-text-tertiary">{label}</span>
        <span className="text-text-primary normal-case">{total} records</span>
      </div>
      <div className="flex h-3 gap-0.5">
        {segments.map((s) => (
          <span key={s.key} className={cn("h-3 rounded-xs", s.className)} style={{ flexGrow: s.count }} />
        ))}
        {segments.length === 0 && <span className="h-3 flex-1 rounded-xs bg-state-soft" />}
      </div>
      <div className="flex items-center gap-5">
        {segments.map((s) => (
          <span key={s.key} className="flex items-center gap-1.5 text-code-11">
            <span className={cn("size-1.5 rounded-full", s.className)} />
            <span className="text-text-primary">{s.count}</span>
            <span className="text-text-tertiary">{s.label}</span>
          </span>
        ))}
      </div>
    </div>
  );
}

/**
 * Figma `Sync / Status Content` (779:206 Connected, 779:207 Disconnected):
 * a 36px header — "Sync", a connection chip with a dot, the endpoint host,
 * then Disconnect and Sync now ⌘R at the right — over surface-well cards:
 * the session (account · device · endpoint · session), the last sync's push
 * and pull meters, and a conflicts banner that opens the first one. The
 * sidebar lists the device pair and every conflict.
 */
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

  useSidebarContent(
    <SyncSidebar
      connection={connection}
      conflicts={conflicts}
      selectedId={resolving}
      onSelect={setResolving}
    />,
  );

  const resolvingConflict = conflicts.find((c) => c.recordId === resolving);
  if (resolving) {
    return (
      <ResolvePanel
        recordId={resolving}
        detectedAt={resolvingConflict?.detectedAt ?? ""}
        onClose={() => setResolving(null)}
        onResolved={() => {
          setResolving(null);
          refreshConflicts().catch((caught) => setError(errorMessage(caught)));
        }}
      />
    );
  }

  const host = (() => {
    try {
      return new URL(connection.endpoint || productionGatewayEndpoint).host;
    } catch {
      return connection.endpoint || productionGatewayEndpoint;
    }
  })();

  return (
    <div
      className="flex h-full flex-col gap-3 overflow-y-auto px-6 pt-5 pb-6"
      onKeyDown={(e) => {
        if ((e.metaKey || e.ctrlKey) && e.key === "r" && connection.connected && !busy) {
          e.preventDefault();
          void onSyncNow();
        }
      }}
    >
      <div className="flex h-9 shrink-0 items-center gap-3">
        <h1 className="text-title-16 text-text-primary">Sync</h1>
        <span className="flex h-6 items-center gap-1.5 rounded-md border border-border-subtle bg-surface-raised px-2.5 text-code-11 text-text-secondary">
          <span className={cn("size-1.5 rounded-full", connection.connected ? "bg-status-success" : "bg-text-tertiary")} />
          {connection.connected ? "connected" : "not connected"}
        </span>
        {connection.connected && (
          <span className="flex h-6 items-center rounded-md border border-border-subtle bg-surface-raised px-2.5 text-code-11 text-text-secondary">
            {host}
          </span>
        )}
        <span className="flex-1" />
        {connection.connected ? (
          <>
            <Button variant="outline" className="bg-surface-raised" onClick={onDisconnect} disabled={busy}>
              Disconnect
            </Button>
            <Button onClick={onSyncNow} disabled={busy}>
              Sync now
              <span className="text-code-11 opacity-65">⌘R</span>
            </Button>
          </>
        ) : (
          <>
            {authentication.authenticated && (
              <Button variant="ghost" onClick={onLogout} disabled={busy}>
                Sign out
              </Button>
            )}
            <Button onClick={onConnect} disabled={busy}>
              {authentication.authenticated ? "Connect" : "Sign in and connect"}
            </Button>
          </>
        )}
      </div>

      {connection.connected ? (
        <div className={cn(CARD, "gap-5 p-5")}>
          <div className="flex gap-10">
            <Field label="Account" value={connection.accountId || "—"} />
            <Field label="Device" value={connection.deviceId || "—"} mono />
          </div>
          <div className="flex gap-10">
            <Field label="Endpoint" value={connection.endpoint} mono />
            <Field
              label="Session"
              value={`refreshes automatically${connection.expiresAt ? ` · expires ${connection.expiresAt}` : ""}`}
            />
          </div>
        </div>
      ) : (
        <>
          <div className={cn(CARD, "gap-3.5 px-5 py-7")}>
            <span className="flex items-center gap-2.5">
              <ShieldCheck className="size-4 text-text-secondary" />
              <span className="text-body-14-medium text-text-primary">
                This vault is not linked to an account
              </span>
            </span>
            <p className="text-body-13 text-text-secondary">
              Local changes are queued and encrypted on this device before anything leaves it. On the
              first sync, this vault is bound to the account you sign in as.
            </p>
            <p className="text-body-13 text-text-tertiary">
              {authentication.authenticated
                ? `Signed in${authentication.expiresAt ? ` until ${authentication.expiresAt}` : ""}. Tokens refresh automatically.`
                : "Sign in opens Keycloak in your browser. DevKit stores only the rotating refresh token, in the OS keychain — never your password."}
            </p>
          </div>
          <div className={cn(CARD, "p-5")}>
            <div className="flex gap-10">
              <Field label="Endpoint" value={productionGatewayEndpoint} mono />
              <Field label="Status" value="read-only — set at build time" />
            </div>
          </div>
        </>
      )}

      {report && (
        <div className={cn(CARD, "gap-6 p-5")}>
          <div className="flex items-center gap-2 text-label-11-caps text-text-tertiary">
            <span className="flex-1">Last sync</span>
            <span className="normal-case">just now</span>
          </div>
          <Meter label="Push" total={report.pushAttempted} segments={pushSegments(report)} />
          <Meter label="Pull" total={report.pulled} segments={pullSegments(report)} />
        </div>
      )}

      {conflicts.length > 0 && (
        <div className="flex items-center gap-3 rounded-md border border-border-subtle bg-status-info/8 px-5 py-4">
          <TriangleAlert className="size-4 shrink-0 text-text-secondary" />
          <span className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="text-body-13-medium text-text-primary">
              {conflicts.length} conflict{conflicts.length === 1 ? " is" : "s are"} holding back{" "}
              {conflicts.length} record{conflicts.length === 1 ? "" : "s"}
            </span>
            <span className="text-body-12 text-text-secondary">
              Nothing is lost — both versions are kept until you choose. Pick one in the sidebar.
            </span>
          </span>
          <Button variant="outline" className="bg-surface-raised" onClick={() => setResolving(conflicts[0].recordId)}>
            Resolve first
          </Button>
        </div>
      )}

      {error ? (
        <p role="alert" className="text-body-13 text-status-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}
