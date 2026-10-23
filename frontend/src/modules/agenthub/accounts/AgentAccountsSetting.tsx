import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Skeleton } from "@/components/ui/skeleton";
import {
  agentAccountPolicySet,
  agentAccountsAdd,
  agentAccountsRefreshStatus,
  agentAccountsRefreshUsage,
  agentAccountsRemove,
  agentAccountsRename,
  agentAccountsSetShared,
  type VendorPolicy,
} from "@/bridge/agentAccounts";
import { errorMessage } from "@/bridge/errors";
import { AccountPolicyEditor } from "./AccountPolicyEditor";
import { AccountSignIn } from "./AccountSignIn";
import { AccountsEditor } from "./AccountsEditor";
import { attempt, dropAccount, replaceAccount } from "./accountActions";
import { movedOrder, refreshNote } from "./accountModel";
import { vendorPolicyOf, withVendorPolicy } from "./accountPolicy";
import {
  AGENT_ACCOUNT_POLICY_KEY,
  agentAccountsKey,
  useAgentAccountPolicy,
  useAgentAccounts,
  useNow,
} from "./useAgentAccounts";
import { vendorInfo } from "../vendors";

/**
 * Settings → Agents → Accounts & limits, for one vendor: its accounts and the
 * rules that move a session between them. Reads and saves through the UI-only
 * bridge methods; the two editors below only draw.
 */
export function AgentAccountsSetting({ vendor }: { vendor: string }) {
  const client = useQueryClient();
  const vendorName = vendorInfo(vendor).name;
  const key = agentAccountsKey(vendor);
  const accounts = useAgentAccounts(vendor);
  const policy = useAgentAccountPolicy();
  const now = useNow();
  const [busyId, setBusyId] = useState("");
  const [error, setError] = useState("");
  const [refreshNotes, setRefreshNotes] = useState<Record<string, string>>({});
  const [savingPolicy, setSavingPolicy] = useState(false);
  const [policyError, setPolicyError] = useState("");
  const list = accounts.data;

  /** One change at a time per account: the buttons are off while it runs. */
  async function change<T>(id: string, run: () => Promise<T>): Promise<{ ok: true; value: T } | { ok: false }> {
    setBusyId(id);
    setError("");
    const outcome = await attempt(client, key, run);
    setBusyId("");
    if (outcome.ok) return outcome;
    setError(outcome.message);
    return { ok: false };
  }

  /** Stores one vendor's rules; a stale revision is fixed by reading again. */
  async function savePolicy(next: VendorPolicy, acknowledgeRisk: boolean, report: (message: string) => void): Promise<boolean> {
    if (!policy.data) return false;
    setSavingPolicy(true);
    report("");
    const outcome = await attempt(client, AGENT_ACCOUNT_POLICY_KEY, () =>
      agentAccountPolicySet(withVendorPolicy(policy.data!, vendor, next), acknowledgeRisk),
    );
    setSavingPolicy(false);
    if (!outcome.ok) {
      report(outcome.message);
      return false;
    }
    client.setQueryData(AGENT_ACCOUNT_POLICY_KEY, outcome.value);
    return true;
  }

  const current = vendorPolicyOf(policy.data, vendor);

  return (
    <div className="flex flex-col gap-6">
      <AccountsEditor
        vendor={vendor}
        vendorName={vendorName}
        accounts={list}
        order={current.order}
        loadError={accounts.error ? errorMessage(accounts.error) : ""}
        busyId={busyId}
        error={error}
        refreshNotes={refreshNotes}
        now={now}
        renderSignIn={(account) => <AccountSignIn account={account} />}
        onAdd={async (label) => {
          const done = await change("add", () => agentAccountsAdd(vendor, label));
          if (done.ok) void client.invalidateQueries({ queryKey: key });
          return done.ok;
        }}
        onRename={async (id, label) => {
          const done = await change(id, () => agentAccountsRename(id, label));
          if (done.ok) replaceAccount(client, key, done.value);
          return done.ok;
        }}
        onRemove={async (id) => {
          const done = await change(id, () => agentAccountsRemove(id));
          if (done.ok) dropAccount(client, key, id);
          return done.ok;
        }}
        onSetShared={(id, shared) =>
          void change(id, () => agentAccountsSetShared(id, shared)).then((done) => done.ok && replaceAccount(client, key, done.value))
        }
        onRefreshUsage={(id) => {
          const before = list?.find((account) => account.id === id);
          setRefreshNotes((notes) => ({ ...notes, [id]: "" }));
          void change(id, () => agentAccountsRefreshUsage(id)).then((done) => {
            if (!done.ok) return;
            replaceAccount(client, key, done.value);
            setRefreshNotes((notes) => ({ ...notes, [id]: before ? refreshNote(before, done.value) : "" }));
          });
        }}
        onRefreshStatus={(id) =>
          void change(id, () => agentAccountsRefreshStatus(id)).then((done) => done.ok && replaceAccount(client, key, done.value))
        }
        onMove={(id, delta) => {
          if (!list || !policy.data) return;
          setBusyId(id);
          setError("");
          void savePolicy({ ...current, order: movedOrder(list, current.order, id, delta) }, false, setError).then(() => setBusyId(""));
        }}
      />

      {policy.isPending ? <Skeleton className="h-48 w-full rounded-control" /> : null}
      {policy.error ? (
        <p className="text-body-12 text-status-danger" role="alert">
          {errorMessage(policy.error)}
        </p>
      ) : null}
      {policy.data ? (
        <AccountPolicyEditor
          vendorName={vendorName}
          policy={current}
          signedInCount={list?.filter((account) => account.signedIn).length ?? 0}
          riskAcknowledged={Boolean(policy.data.riskAcknowledgedAt.trim())}
          saving={savingPolicy}
          error={policyError}
          onSave={(next, acknowledgeRisk) => savePolicy(next, acknowledgeRisk, setPolicyError)}
        />
      ) : null}
    </div>
  );
}
