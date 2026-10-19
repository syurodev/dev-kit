import { Fragment, useState } from "react";
import type { Account } from "@/bridge/accounts";
import { Button } from "@/components/ui/button";
import { AccountRow } from "@/modules/accounts/AccountsView";
import { groupAccounts } from "@/modules/accounts/accountGroups";
import { EditAccountDialog } from "@/modules/accounts/EditAccountDialog";

/**
 * Accounts: a key drawn with its API token, and the Edit dialog in each state
 * (key with no token, key with one, a token that serves a key, logins that
 * differ). Bridge-free: the buttons inside the dialog call the bridge, which
 * answers with an error here. `?gallery#accounts-links`.
 */
const base: Omit<Account, "id" | "kind" | "label"> = {
  provider: "gitlab",
  subject: "",
  scopes: [],
  env: [],
  scopeOwner: "",
  state: "valid",
  expiresAt: "",
  verifiedAt: "",
  ref: "",
  linkedTokenId: "",
  linkedKeyIds: null,
};

const key = (id: string, label: string, over: Partial<Account> = {}): Account => ({
  ...base,
  id,
  kind: "ssh-key",
  label,
  scopeOwner: `local:${id}`,
  ...over,
});
const token = (id: string, label: string, over: Partial<Account> = {}): Account => ({
  ...base,
  id,
  kind: "token",
  label,
  scopeOwner: `local:${id}`,
  ...over,
});

const bareKey = key("k1", "GitLab · id_ed25519_gitlab", { subject: "chiro" });
const spareToken = token("t0", "Spare GitLab token", { subject: "chiro", scopes: ["read_api"] });
const linkedKey = key("k2", "GitLab · work key", { subject: "chiro", linkedTokenId: "t1" });
const linkedToken = token("t1", "chiro", { subject: "chiro", scopes: ["read_api"], linkedKeyIds: ["k2"] });
const otherKey = key("k3", "GitLab · bot key", { subject: "deploy-bot", linkedTokenId: "t2" });
const otherToken = token("t2", "Personal token", { subject: "ann", linkedKeyIds: ["k3"] });
const github = key("k4", "GitHub · syurodev", { provider: "github", subject: "syurodev" });

const all: Account[] = [bareKey, spareToken, linkedKey, linkedToken, otherKey, otherToken, github];

const noop = () => {};

export function AccountLinksSection() {
  const [open, setOpen] = useState<string | null>(null);
  const cases: { id: string; label: string }[] = [
    { id: "k1", label: "Key with no token (a spare token exists)" },
    { id: "k4", label: "GitHub key with no token" },
    { id: "k2", label: "Key with its token" },
    { id: "t1", label: "The token, seen from its side" },
    { id: "k3", label: "Key and token of different logins" },
  ];
  return (
    <section id="accounts-links" className="flex flex-col gap-6 p-6">
      <h2 className="text-heading-18 text-text-primary">Accounts: a key with its API token</h2>
      <ul className="w-full max-w-170 rounded-control border border-border-subtle">
        {groupAccounts(all).map(({ account, token: linked }) => (
          <Fragment key={account.id}>
            <AccountRow
              account={account}
              onChanged={noop}
              onReveal={noop}
              onShowKey={noop}
              onIdentity={noop}
              onRotate={noop}
              onEdit={() => setOpen(account.id)}
            />
            {linked ? (
              <AccountRow
                nested
                account={linked}
                onChanged={noop}
                onReveal={noop}
                onShowKey={noop}
                onIdentity={noop}
                onRotate={noop}
                onEdit={() => setOpen(linked.id)}
              />
            ) : null}
          </Fragment>
        ))}
      </ul>
      <div className="flex flex-wrap gap-2">
        {cases.map((c) => (
          <Button key={c.id} variant="outline" size="sm" onClick={() => setOpen(c.id)}>
            {c.label}
          </Button>
        ))}
      </div>
      <EditAccountDialog
        account={all.find((a) => a.id === open) ?? null}
        accounts={all}
        onClose={() => setOpen(null)}
        onChanged={noop}
        onReplaceToken={noop}
      />
    </section>
  );
}
