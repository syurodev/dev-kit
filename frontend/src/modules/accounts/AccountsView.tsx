import { useCallback, useEffect, useState } from "react";
import { BadgeCheck, Eye, KeyRound, RefreshCw, ShieldAlert, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import {
  deleteAccount,
  listAccounts,
  migrateProfileCredentials,
  promoteAccount,
  stopSyncingProfileSecrets,
  verifyAccount,
  type Account,
} from "@/bridge/accounts";
import { RevealDialog } from "./RevealDialog";

const STATE_LABEL: Record<Account["state"], string> = {
  valid: "Valid",
  expiring: "Expiring soon",
  expired: "Expired",
  revoked: "Revoked",
  unverified: "Not verified",
  needs_local_secret: "Needs the password for this machine",
};

const STATE_TONE: Record<Account["state"], string> = {
  valid: "text-fg-muted",
  expiring: "text-amber-500",
  expired: "text-red-500",
  revoked: "text-red-500",
  unverified: "text-fg-muted",
  needs_local_secret: "text-amber-500",
};

function AccountRow({
  account,
  onChanged,
  onReveal,
}: {
  account: Account;
  onChanged: () => void;
  onReveal: () => void;
}) {
  const [error, setError] = useState("");

  return (
    <li className="flex items-center gap-3 border-b border-border-subtle px-3 py-2 last:border-b-0">
      <KeyRound className="size-4 shrink-0 text-fg-muted" aria-hidden />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm">
          {account.label}
          {account.subject ? (
            <span className="text-fg-muted"> · {account.subject}</span>
          ) : null}
        </div>
        <div className="truncate text-xs text-fg-muted">
          {account.provider}
          {account.scopes?.length ? ` · ${account.scopes.join(", ")}` : ""}
          {account.env?.length ? ` · ${account.env.join(", ")}` : ""}
        </div>
        {error ? <div className="text-xs text-red-500">{error}</div> : null}
      </div>
      <span className={`shrink-0 text-xs ${STATE_TONE[account.state]}`}>
        {STATE_LABEL[account.state]}
      </span>
      {!account.scopeOwner && account.kind !== "external" ? (
        <Button
          variant="ghost"
          size="icon"
          aria-label={`Verify ${account.label}`}
          title="Ask the provider who this credential belongs to"
          onClick={() => {
            verifyAccount(account.id)
              .then(onChanged)
              .catch((e: Error) => setError(e.message));
          }}
        >
          <BadgeCheck className="size-4" aria-hidden />
        </Button>
      ) : null}
      {/* An external account records that an identity exists somewhere DevKit
          does not hold it, so there is nothing to show. */}
      {account.kind !== "external" && account.state !== "needs_local_secret" ? (
        <Button
          variant="ghost"
          size="icon"
          aria-label={`Show the value for ${account.label}`}
          title="Show the stored value"
          onClick={onReveal}
        >
          <Eye className="size-4" aria-hidden />
        </Button>
      ) : null}
      {account.scopeOwner ? (
        <Button
          variant="ghost"
          size="sm"
          title="Make this account shared, so it syncs to your other devices"
          onClick={() => {
            promoteAccount(account.id, account.label)
              .then(onChanged)
              .catch((e: Error) => setError(e.message));
          }}
        >
          Promote
        </Button>
      ) : null}
      <Button
        variant="ghost"
        size="icon"
        aria-label={`Delete ${account.label}`}
        onClick={() => {
          deleteAccount(account.id)
            .then(onChanged)
            .catch((e: Error) => setError(e.message));
        }}
      >
        <Trash2 className="size-4" aria-hidden />
      </Button>
    </li>
  );
}

/**
 * One list answering "what am I signed in to, and what is about to break".
 *
 * Shared accounts first; scoped ones — a single connection's password — sit in
 * their own group underneath, because otherwise nine of twelve rows would be
 * database passwords and the question above would be unanswerable.
 */
export function AccountsView() {
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [error, setError] = useState("");
  // A migration that copied nothing and one that copied twelve both succeed;
  // saying which is the whole value of reporting it.
  const [notice, setNotice] = useState("");
  // The account whose value is on screen, or null. One at a time, always.
  const [revealing, setRevealing] = useState<Account | null>(null);

  const reload = useCallback(() => {
    listAccounts()
      .then((next) => {
        setAccounts(next);
        setError("");
      })
      .catch((e: Error) => setError(e.message));
  }, []);

  useEffect(reload, [reload]);

  if (error) {
    return (
      <EmptyState
        icon={ShieldAlert}
        title="Accounts are unavailable"
        body={error}
      />
    );
  }
  if (!accounts) {
    return null;
  }
  if (accounts.length === 0) {
    return (
      <EmptyState
        icon={KeyRound}
        title="No accounts yet"
        body="Add a token or an API key here and other modules can use it by name — rotating it later is one edit, in one place."
      />
    );
  }

  const shared = accounts.filter((account) => !account.scopeOwner);
  const scoped = accounts.filter((account) => account.scopeOwner);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between border-b border-border-subtle px-3 py-2">
        <h2 className="text-sm font-medium">Accounts</h2>
        <div className="flex-1" />
        <Button
          variant="ghost"
          size="sm"
          title="Copy database, SSH and Kafka passwords into Accounts. Safe to run on every device — nothing is removed."
          onClick={() => {
            migrateProfileCredentials()
              .then((report) => {
                setNotice(
                  report.copied === 0
                    ? "Every connection password is already here."
                    : `Copied ${report.copied} connection password(s) into Accounts. They are still synced until you stop that below.`,
                );
                reload();
              })
              .catch((e: Error) => setError(e.message));
          }}
        >
          Import connection passwords
        </Button>
        <Button
          variant="ghost"
          size="sm"
          title="Connection passwords stop syncing. Your other devices will ask for them again."
          onClick={() => {
            stopSyncingProfileSecrets()
              .then((report) => {
                setNotice(
                  report.cleared === 0
                    ? "Nothing to stop syncing yet — import them first."
                    : `${report.cleared} connection password(s) now stay on this machine. Your other devices will ask for them again.`,
                );
                reload();
              })
              .catch((e: Error) => setError(e.message));
          }}
        >
          Stop syncing them
        </Button>
        <Button variant="ghost" size="icon" aria-label="Reload" onClick={reload}>
          <RefreshCw className="size-4" aria-hidden />
        </Button>
      </div>
      {notice ? <p className="px-3 py-1 text-xs text-fg-muted">{notice}</p> : null}
      <div className="min-h-0 flex-1 overflow-auto">
        <ul>
          {shared.map((account) => (
            <AccountRow
              key={account.id}
              account={account}
              onChanged={reload}
              onReveal={() => setRevealing(account)}
            />
          ))}
        </ul>
        {scoped.length > 0 ? (
          <details className="border-t border-border-subtle">
            <summary className="cursor-pointer px-3 py-2 text-xs text-fg-muted">
              Belonging to one connection ({scoped.length}) · these stay on this
              machine
            </summary>
            <ul>
              {scoped.map((account) => (
                <AccountRow
                  key={account.id}
                  account={account}
                  onChanged={reload}
                  onReveal={() => setRevealing(account)}
                />
              ))}
            </ul>
          </details>
        ) : null}
      </div>
      <RevealDialog account={revealing} onClose={() => setRevealing(null)} />
    </div>
  );
}
