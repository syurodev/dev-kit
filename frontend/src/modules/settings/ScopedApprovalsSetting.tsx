import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getScopedApprovals, revokeScopedApproval, type ScopedApproval } from "@/bridge/interaction";
import { errorMessage } from "@/bridge/errors";
import { Button } from "@/components/ui/button";

const APPROVALS_KEY = ["agents", "scoped-approvals"] as const;
const NO_APPROVALS: ScopedApproval[] = [];

function expiry(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "unknown" : date.toLocaleString();
}

export function ScopedApprovalsSetting() {
  const client = useQueryClient();
  const approvals = useQuery({
    queryKey: APPROVALS_KEY,
    queryFn: getScopedApprovals,
    refetchOnMount: "always",
  });
  const revoke = useMutation({
    mutationFn: revokeScopedApproval,
    onSuccess: async (_, id) => {
      // An older refresh must not put a revoked approval back in the list.
      await client.cancelQueries({ queryKey: APPROVALS_KEY });
      client.setQueryData<ScopedApproval[]>(APPROVALS_KEY, (current) => current?.filter((item) => item.id !== id));
      await client.invalidateQueries({ queryKey: APPROVALS_KEY });
    },
  });
  const items = approvals.data ?? NO_APPROVALS;
  const error = revoke.error ?? approvals.error;

  return <section className="rounded-xl border border-border-subtle bg-background p-4">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div><h3 className="text-body-14 font-medium text-text-primary">Active approvals</h3>
        <p className="mt-1 max-w-2xl text-body-12 text-text-secondary">An approval is tied to one requester, database profile, environment and exact SQL. Environment confirmation still asks every time. Approvals expire on restart or when the vault is locked.</p>
      </div>
      <Button type="button" variant="outline" size="sm" disabled={approvals.isFetching} onClick={() => { revoke.reset(); void approvals.refetch(); }}>Refresh</Button>
    </div>
    {error && <p role="alert" className="mt-3 text-body-12 text-status-danger">{errorMessage(error)}</p>}
    {items.length === 0 && approvals.isSuccess && !error ? <p className="mt-4 rounded-lg border border-dashed border-border-subtle p-4 text-body-12 text-text-tertiary">No active scoped approvals.</p>
      : items.length === 0 && approvals.isPending ? <p className="mt-4 text-body-12 text-text-tertiary">Loading approvals…</p>
      : <ul className="mt-4 divide-y divide-border-subtle">
        {items.map((item) => <li key={item.id} className="flex flex-wrap items-center gap-3 py-3 first:pt-0 last:pb-0">
          <div className="min-w-0 flex-1">
            <p className="truncate text-body-13 font-medium text-text-primary">{item.tool} · {item.env} · {item.profileId}</p>
            <p className="mt-0.5 truncate text-caption-11 text-text-secondary" title={`${item.caller} · query fingerprint ${item.fingerprint}`}>{item.caller} · query {item.fingerprint.slice(0, 12)} · {item.remaining} use{item.remaining === 1 ? "" : "s"} left · {item.lastUsedAt ? `last used ${expiry(item.lastUsedAt)} · ` : ""}expires {expiry(item.expiresAt)}</p>
          </div>
          <Button type="button" variant="outline" size="sm" disabled={revoke.isPending} onClick={() => revoke.mutate(item.id)}>{revoke.isPending && revoke.variables === item.id ? "Revoking…" : "Revoke"}</Button>
        </li>)}
      </ul>}
  </section>;
}
