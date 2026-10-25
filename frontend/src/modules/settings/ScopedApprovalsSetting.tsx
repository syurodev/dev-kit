import { useCallback, useEffect, useState } from "react";
import { getScopedApprovals, revokeScopedApproval, type ScopedApproval } from "@/bridge/interaction";
import { errorMessage } from "@/bridge/errors";
import { Button } from "@/components/ui/button";

function expiry(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "unknown" : date.toLocaleString();
}

export function ScopedApprovalsSetting() {
  const [items, setItems] = useState<ScopedApproval[]>([]);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);

  const refresh = useCallback(async () => {
    try { setItems(await getScopedApprovals()); setError(""); }
    catch (e) { setError(errorMessage(e)); }
    finally { setLoaded(true); }
  }, []);
  useEffect(() => { void refresh(); }, [refresh]);

  async function revoke(id: string) {
    setBusy(id); setError("");
    try { await revokeScopedApproval(id); setItems((current) => current.filter((item) => item.id !== id)); await refresh(); }
    catch (e) { setError(errorMessage(e)); }
    finally { setBusy(""); }
  }

  return <section className="rounded-xl border border-border-subtle bg-background p-4">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div><h3 className="text-body-14 font-medium text-text-primary">Active approvals</h3>
        <p className="mt-1 max-w-2xl text-body-12 text-text-secondary">An approval is tied to one requester, database profile, environment and exact SQL. Environment confirmation still asks every time. Approvals expire on restart or when the vault is locked.</p>
      </div>
      <Button type="button" variant="outline" size="sm" onClick={() => void refresh()}>Refresh</Button>
    </div>
    {error && <p role="alert" className="mt-3 text-body-12 text-status-danger">{error}</p>}
    {items.length === 0 && loaded && !error ? <p className="mt-4 rounded-lg border border-dashed border-border-subtle p-4 text-body-12 text-text-tertiary">No active scoped approvals.</p>
      : items.length === 0 && !loaded ? <p className="mt-4 text-body-12 text-text-tertiary">Loading approvals…</p>
      : <ul className="mt-4 divide-y divide-border-subtle">
        {items.map((item) => <li key={item.id} className="flex flex-wrap items-center gap-3 py-3 first:pt-0 last:pb-0">
          <div className="min-w-0 flex-1">
            <p className="truncate text-body-13 font-medium text-text-primary">{item.tool} · {item.env} · {item.profileId}</p>
            <p className="mt-0.5 truncate text-caption-11 text-text-secondary" title={`${item.caller} · query fingerprint ${item.fingerprint}`}>{item.caller} · query {item.fingerprint.slice(0, 12)} · {item.remaining} use{item.remaining === 1 ? "" : "s"} left · {item.lastUsedAt ? `last used ${expiry(item.lastUsedAt)} · ` : ""}expires {expiry(item.expiresAt)}</p>
          </div>
          <Button type="button" variant="outline" size="sm" disabled={busy === item.id} onClick={() => void revoke(item.id)}>{busy === item.id ? "Revoking…" : "Revoke"}</Button>
        </li>)}
      </ul>}
  </section>;
}
