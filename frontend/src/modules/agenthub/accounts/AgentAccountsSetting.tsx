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
  type AccountPolicy,
} from "@/bridge/agentAccounts";
import { errorMessage } from "@/bridge/errors";
import { AccountPolicyEditor } from "./AccountPolicyEditor";
import { AccountSignIn } from "./AccountSignIn";
import { AccountsEditor } from "./AccountsEditor";
import {
  applyUsageRefresh,
  attempt,
  dropAccount,
  moveVendorAccount,
  replaceAccount,
  saveVendorPolicy,
  type Outcome,
} from "./accountActions";
import { vendorPolicyOf } from "./accountPolicy";
import {
  agentAccountsKey,
  useAgentAccountPolicy,
  useAgentAccounts,
  useNow,
} from "./useAgentAccounts";
import { vendorInfo } from "../vendors";

const NOT_LOADED = "The switching rules have not loaded yet.";

/**
 * Settings → Agents → Accounts & limits, for one vendor: its accounts and the
 * rules that move a session between them. Reads and saves through the UI-only
 * bridge methods; the two editors below only draw. The body is keyed by vendor,
 * so a Claude | Codex switch never carries one vendor's busy row, errors,
 * notes or half-edited form over to the other, wherever this is mounted.
 */
export function AgentAccountsSetting({ vendor }: { vendor: string }) {
  return <VendorAccounts key={vendor} vendor={vendor} />;
}

function VendorAccounts({ vendor }: { vendor: string }) {
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
    setBusyId((cur) => (cur === id ? "" : cur));
    if (outcome.ok) return outcome;
    setError(outcome.message);
    return { ok: false };
  }

  /** Runs a policy write with the editor and the rows held still; a refusal is shown through `report`. */
  async function storing(run: (rules: AccountPolicy) => Promise<Outcome<AccountPolicy>>, report: (message: string) => void): Promise<boolean> {
    if (!policy.data) {
      report(NOT_LOADED);
      return false;
    }
    setSavingPolicy(true);
    report("");
    const outcome = await run(policy.data);
    setSavingPolicy(false);
    if (!outcome.ok) report(outcome.message);
    return outcome.ok;
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
            const note = applyUsageRefresh(client, key, before, done.value);
            setRefreshNotes((notes) => ({ ...notes, [id]: note }));
          });
        }}
        onRefreshStatus={(id) =>
          void change(id, () => agentAccountsRefreshStatus(id)).then((done) => done.ok && replaceAccount(client, key, done.value))
        }
        onDismissError={() => setError("")}
        onMove={(id, delta) => {
          // A save of the rules is in flight: its revision would make this one stale.
          if (savingPolicy) return;
          if (!list || !policy.data) {
            setError(NOT_LOADED);
            return;
          }
          setBusyId(id);
          void storing((rules) => moveVendorAccount(client, rules, vendor, list, id, delta, agentAccountPolicySet), setError).then(() =>
            setBusyId((cur) => (cur === id ? "" : cur)),
          );
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
          onSave={(next, acknowledgeRisk) =>
            storing((rules) => saveVendorPolicy(client, rules, vendor, next, acknowledgeRisk, agentAccountPolicySet), setPolicyError)
          }
        />
      ) : null}
    </div>
  );
}
