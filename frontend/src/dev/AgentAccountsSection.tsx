import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { LogIn } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { AgentAccount, VendorPolicy } from "@/bridge/agentAccounts";
import { AccountPolicyEditor } from "@/modules/agenthub/accounts/AccountPolicyEditor";
import { AccountRiskBody } from "@/modules/agenthub/accounts/AccountRiskDialog";
import { AccountsEditor, type AccountsEditorProps } from "@/modules/agenthub/accounts/AccountsEditor";
import { accountSignInNote } from "@/modules/agenthub/accounts/AccountSignIn";
import { agentAccountsKey } from "@/modules/agenthub/accounts/useAgentAccounts";
import { CodexSignInPanel } from "@/modules/agenthub/CodexSignIn";
import { AccountNotice } from "@/modules/agenthub/transcript/AccountNotice";

/**
 * Settings → Agents → Accounts & limits in its states, and the two transcript
 * notices, fed fixtures and no bridge. The editors are the real ones; only the
 * sign-in controls are static, because the live ones start a login. It also seeds
 * the accounts list the conversation section above reads, so its "Switched from"
 * line names Work. `?gallery#agent-accounts`.
 */
const NOW = Date.now();
const sec = NOW / 1000;
const noop = async () => true;

const builtin: AgentAccount = {
  id: "claude-default",
  vendor: "claude",
  label: "System default",
  builtin: true,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [
    { label: "5-hour", usedPercent: 41, resetsAt: sec + 2 * 3600, status: "ok" },
    { label: "Weekly", usedPercent: 22, resetsAt: sec + 4 * 86_400, status: "ok" },
  ],
  usageSource: "observed",
  observedAt: sec - 40 * 60,
};
const work: AgentAccount = {
  ...builtin,
  id: "claude-work",
  label: "Work",
  builtin: false,
  shared: true,
  usage: [
    { label: "5-hour", usedPercent: 83, resetsAt: sec + 90 * 60, status: "warning" },
    { label: "Weekly", usedPercent: 40, resetsAt: sec + 3 * 86_400, status: "ok" },
  ],
  usageSource: "checked",
  observedAt: sec - 2 * 60,
};
const spare: AgentAccount = {
  ...builtin,
  id: "claude-spare",
  label: "Spare",
  builtin: false,
  usage: [{ label: "5-hour", usedPercent: 100, resetsAt: sec + 3 * 3600, status: "reached" }],
  exhaustedUntil: sec + 3 * 3600,
  usageSource: "observed",
  observedAt: sec - 5 * 60,
};
const signedOut: AgentAccount = { ...work, signedIn: false, usage: [], usageSource: "", observedAt: 0 };

const codexBuiltin: AgentAccount = { ...builtin, id: "codex-default", vendor: "codex", label: "DevKit default" };
const codexSecond: AgentAccount = { ...signedOut, id: "codex-personal", vendor: "codex", label: "Personal", shared: false };

const off: VendorPolicy = {
  enabled: false,
  thresholdPercent: 90,
  weeklyThresholdPercent: 0,
  usageMaxAgeSeconds: 300,
  order: [],
  onLimitRefusal: "none",
};
const on: VendorPolicy = { ...off, enabled: true, weeklyThresholdPercent: 95, onLimitRefusal: "resendIfNoProgress", order: ["claude-work", "claude-default", "claude-spare"] };

const SIGN_IN_CLAUDE = (account: AgentAccount) => (
  <div className="flex w-full flex-col gap-2 text-left">
    <div className="flex flex-wrap items-center gap-2">
      <Button type="button" size="sm">
        <LogIn />
        Sign in with Claude subscription
      </Button>
    </div>
    <p className="text-caption-11 text-text-tertiary">{accountSignInNote(account.vendor)}</p>
  </div>
);
const SIGN_IN_CODEX = (account: AgentAccount) => (
  <CodexSignInPanel
    flow={null}
    starting=""
    outcome={null}
    startError=""
    onStart={() => {}}
    onCancel={() => {}}
    note={accountSignInNote(account.vendor)}
  />
);

function editorProps(over: Partial<AccountsEditorProps>): AccountsEditorProps {
  return {
    vendor: "claude",
    vendorName: "Claude",
    accounts: [builtin],
    order: [],
    loadError: "",
    busyId: "",
    error: "",
    refreshNotes: {},
    now: NOW,
    renderSignIn: SIGN_IN_CLAUDE,
    onAdd: noop,
    onRename: noop,
    onRemove: noop,
    onSetShared: () => {},
    onRefreshUsage: () => {},
    onRefreshStatus: () => {},
    onMove: () => {},
    ...over,
  };
}

const frame = "w-full max-w-170 rounded-control border border-border-subtle p-4";
const caption = "mb-2 text-label-12 text-text-tertiary";

export function AgentAccountsSection() {
  const client = useQueryClient();
  // The conversation section names accounts through the same list Settings reads.
  useEffect(() => {
    client.setQueryData(agentAccountsKey("claude"), [builtin, work, spare]);
  }, [client]);
  return (
    <section id="agent-accounts" className="flex flex-col gap-6 p-6">
      <h2 className="text-heading-18 text-text-primary">Accounts & limits</h2>

      <div className={frame}>
        <p className={caption}>Only the built-in account: nothing to reorder or remove, switching off</p>
        <div className="flex flex-col gap-6">
          <AccountsEditor {...editorProps({})} />
          <AccountPolicyEditor vendorName="Claude" policy={off} signedInCount={1} riskAcknowledged={false} saving={false} error="" onSave={noop} />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>Three accounts in a saved order: usage ok, a warning at 83% and one that reached its limit; switching on</p>
        <div className="flex flex-col gap-6">
          <AccountsEditor {...editorProps({ accounts: [builtin, work, spare], order: on.order, refreshNotes: { "claude-work": "Nothing new: DevKit asks the vendor at most every few seconds, and this reading is already the latest." } })} />
          <AccountPolicyEditor vendorName="Claude" policy={on} signedInCount={2} riskAcknowledged saving={false} error="" onSave={noop} />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>Every account at its limit: each one says until when</p>
        <AccountsEditor {...editorProps({ accounts: [{ ...spare, id: "claude-default", label: "System default", builtin: true }, spare] })} />
      </div>

      <div className={frame}>
        <p className={caption}>A signed-out account offers its sign-in (Claude: terminal, Codex: in DevKit)</p>
        <div className="flex flex-col gap-6">
          <AccountsEditor {...editorProps({ accounts: [builtin, signedOut] })} />
          <AccountsEditor
            {...editorProps({ vendor: "codex", vendorName: "Codex", accounts: [codexBuiltin, codexSecond], renderSignIn: SIGN_IN_CODEX })}
          />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>Add account: the form, and while it adds</p>
        <div className="flex flex-col gap-6">
          <AccountsEditor {...editorProps({ initialAddOpen: true })} />
          <AccountsEditor {...editorProps({ initialAddOpen: true, busyId: "add" })} />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>A change Go refused, and a list that could not be read</p>
        <div className="flex flex-col gap-6">
          <AccountsEditor {...editorProps({ accounts: [builtin, work], error: "A session is using that account. Close it first." })} />
          <AccountsEditor {...editorProps({ accounts: undefined, loadError: "Accounts are not available." })} />
          <AccountsEditor {...editorProps({ accounts: undefined })} />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>Switching on with one signed-in account, and a save the store refused</p>
        <div className="flex flex-col gap-6">
          <AccountPolicyEditor vendorName="Claude" policy={{ ...on, order: [] }} signedInCount={1} riskAcknowledged saving={false} error="" onSave={noop} />
          <AccountPolicyEditor
            vendorName="Claude"
            policy={on}
            signedInCount={2}
            riskAcknowledged
            saving={false}
            error="The account switching rules changed. Refresh and apply your change again."
            onSave={noop}
          />
        </div>
      </div>

      <div className={frame}>
        <p className={caption}>The terms warning, shown the first time switching is turned on (try the switch in the first card)</p>
        <AccountRiskBody checked={false} onCheckedChange={() => {}} />
      </div>

      <div className={frame}>
        <p className={caption}>In the transcript: a move that stood, and a move that did not happen</p>
        <div className="flex flex-col gap-2">
          <AccountNotice notice={{ kind: "switch", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" }} fromLabel="System default" toLabel="Work" />
          <AccountNotice notice={{ kind: "failed", from: "claude-default", to: "claude-spare", reason: "That account could not be opened." }} fromLabel="System default" toLabel="Spare" />
        </div>
      </div>
    </section>
  );
}
