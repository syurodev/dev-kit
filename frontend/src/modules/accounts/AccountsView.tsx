import { BrandIcon, isBrand } from "@/components/brand-icon/BrandIcon";
import { useCallback, useEffect, useState } from "react";
import { BadgeCheck, Download, Eye, Fingerprint, KeyRound, Plus, RefreshCw, RotateCw, ShieldAlert, Trash2, UserRound } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import {
  deleteAccount,
  isLocalAccount,
  listAccounts,
  migrateProfileCredentials,
  promoteAccount,
  stopSyncingProfileSecrets,
  verifyAccount,
  type Account,
} from "@/bridge/accounts";
import { AddSSHKeyDialog } from "./AddSSHKeyDialog";
import { AddTokenDialog } from "./AddTokenDialog";
import { useAddTokenRequest } from "@/stores/addTokenRequest";
import { ImportSSHKeysDialog } from "./ImportSSHKeysDialog";
import { IdentityDialog } from "./IdentityDialog";
import { canRotateToken } from "./gitTokens";
import { RevealDialog } from "./RevealDialog";
import { RotateTokenDialog } from "./RotateTokenDialog";
import { SSHKeyInfoDialog } from "./SSHKeyInfoDialog";

const STATE_LABEL: Record<Account["state"], string> = {
  valid: "Valid",
  expiring: "Expiring soon",
  expired: "Expired",
  revoked: "Revoked",
  unverified: "Not verified",
  needs_local_secret: "Needs the password for this machine",
};

const STATE_TONE: Record<Account["state"], string> = {
  valid: "text-text-tertiary",
  expiring: "text-status-warning",
  expired: "text-status-danger",
  revoked: "text-status-danger",
  unverified: "text-text-tertiary",
  needs_local_secret: "text-status-warning",
};

function AccountRow({
  account,
  onChanged,
  onReveal,
  onShowKey,
  onIdentity,
  onRotate,
}: {
  account: Account;
  onChanged: () => void;
  onReveal: () => void;
  onShowKey: () => void;
  onIdentity: () => void;
  onRotate: () => void;
}) {
  // A local account belongs to no connection: it is the person's own, kept on
  // this machine. It verifies and promotes like a shared one, so only a
  // connection's password is treated as "scoped" here.
  const local = isLocalAccount(account);
  const isKey = account.kind === "ssh-key";
  // Only an account git can use commits as anyone: a connection's password or
  // an external sign-in does not.
  const commits = ["github", "gitlab", "bitbucket"].includes(account.provider) && (!account.scopeOwner || local);
  const [error, setError] = useState("");

  return (
    <li className="flex items-center gap-3 border-b border-border-subtle px-3 py-2 last:border-b-0">
      {/* Which host the credential is for, when DevKit has its mark; the key for the rest. */}
      {isBrand(account.provider) ? (
        <BrandIcon brand={account.provider} className="text-text-tertiary" />
      ) : (
        <KeyRound className="size-4 shrink-0 text-text-tertiary" aria-hidden />
      )}
      <div className="min-w-0 flex-1">
        <div className="truncate text-body-13-medium text-text-primary">
          {account.label}
          {account.subject ? (
            <span className="font-normal text-text-secondary"> · {account.subject}</span>
          ) : null}
        </div>
        <div className="truncate text-caption-11 text-text-tertiary">
          {account.provider}
          {account.scopes?.length ? ` · ${account.scopes.join(", ")}` : ""}
          {account.env?.length ? ` · ${account.env.join(", ")}` : ""}
          {local ? " · this machine only" : ""}
          {account.authorEmail ? ` · commits as ${account.authorName} <${account.authorEmail}>` : ""}
        </div>
        {error ? <div className="text-caption-11 text-status-danger">{error}</div> : null}
      </div>
      <span className={`shrink-0 text-caption-11-medium ${STATE_TONE[account.state]}`}>
        {STATE_LABEL[account.state]}
      </span>
      {(!account.scopeOwner || local) && account.kind !== "external" ? (
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
      {canRotateToken(account) ? (
        <Button
          variant="ghost"
          size="icon"
          aria-label={`Replace the token of ${account.label}`}
          title="Replace the token in place. Everything bound to this account keeps working."
          onClick={onRotate}
        >
          <RotateCw className="size-4" aria-hidden />
        </Button>
      ) : null}
      {commits ? (
        <Button
          variant="ghost"
          size="icon"
          aria-label={`Commit identity for ${account.label}`}
          title={
            account.authorEmail
              ? "Change the name and email commits are written with"
              : "Set the name and email commits are written with"
          }
          onClick={onIdentity}
        >
          <UserRound className="size-4" aria-hidden />
        </Button>
      ) : null}
      {isKey ? (
        <Button
          variant="ghost"
          size="icon"
          aria-label={`Show the public key for ${account.label}`}
          title="Show the fingerprint and the public key to add to the provider"
          onClick={onShowKey}
        >
          <Fingerprint className="size-4" aria-hidden />
        </Button>
      ) : null}
      {/* An external account records that an identity exists somewhere DevKit
          does not hold it, so there is nothing to show. A private SSH key is
          never shown again either: its public half is what is useful. */}
      {account.kind !== "external" && !isKey && account.state !== "needs_local_secret" ? (
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
          title={
            local
              ? "Sync this key to your other devices. It will be sealed in your vault; there is no way back."
              : "Make this account shared, so it syncs to your other devices"
          }
          onClick={() => {
            promoteAccount(account.id, account.label)
              .then(onChanged)
              .catch((e: Error) => setError(e.message));
          }}
        >
          {local ? "Sync" : "Promote"}
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
  const [showingKey, setShowingKey] = useState<Account | null>(null);
  const [editingIdentity, setEditingIdentity] = useState<Account | null>(null);
  const [rotating, setRotating] = useState<Account | null>(null);
  const [adding, setAdding] = useState(false);
  const [importing, setImporting] = useState(false);
  const [addingToken, setAddingToken] = useState(false);
  // Another module asked for a token form for one provider (a pull-request notice: "add a GitLab token").
  const [tokenProvider, setTokenProvider] = useState<string | undefined>(undefined);
  const requestedProvider = useAddTokenRequest((state) => state.provider);
  useEffect(() => {
    if (requestedProvider === null) return;
    const provider = useAddTokenRequest.getState().take();
    if (provider === null) return;
    setTokenProvider(provider || undefined);
    setAddingToken(true);
  }, [requestedProvider]);

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
  const shared = accounts.filter((account) => !account.scopeOwner || isLocalAccount(account));
  const scoped = accounts.filter((account) => account.scopeOwner && !isLocalAccount(account));

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between border-b border-border-subtle px-3 py-2">
        <h2 className="text-title-13 text-text-primary">Accounts</h2>
        <div className="flex-1" />
        <Button
          variant="ghost"
          size="sm"
          title="Add a personal access token for repositories cloned over HTTPS"
          onClick={() => {
            setTokenProvider(undefined);
            setAddingToken(true);
          }}
        >
          <Plus className="size-4" aria-hidden />
          Add token
        </Button>
        <Button
          variant="ghost"
          size="sm"
          title="Bring a key from ~/.ssh into an account. The file is left as it is."
          onClick={() => setImporting(true)}
        >
          <Download className="size-4" aria-hidden />
          Import from ~/.ssh
        </Button>
        <Button
          variant="ghost"
          size="sm"
          title="Add a private key that signs git over SSH"
          onClick={() => setAdding(true)}
        >
          <Plus className="size-4" aria-hidden />
          Add SSH key
        </Button>
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
      {notice ? <p className="px-3 py-1 text-caption-11 text-text-tertiary">{notice}</p> : null}
      <div className="min-h-0 flex-1 overflow-auto">
        {accounts.length === 0 ? (
          <EmptyState
            icon={KeyRound}
            title="No accounts yet"
            body="Add an SSH key or an access token here and git uses it for the repositories you choose it for — one account per identity, picked by who owns the repository."
          />
        ) : null}
        <ul>
          {shared.map((account) => (
            <AccountRow
              key={account.id}
              account={account}
              onChanged={reload}
              onReveal={() => setRevealing(account)}
              onShowKey={() => setShowingKey(account)}
              onIdentity={() => setEditingIdentity(account)}
              onRotate={() => setRotating(account)}
            />
          ))}
        </ul>
        {scoped.length > 0 ? (
          <details className="border-t border-border-subtle">
            <summary className="cursor-pointer px-3 py-2 text-caption-11 text-text-tertiary">
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
                  onShowKey={() => setShowingKey(account)}
              onIdentity={() => setEditingIdentity(account)}
              onRotate={() => setRotating(account)}
                />
              ))}
            </ul>
          </details>
        ) : null}
      </div>
      <RevealDialog account={revealing} onClose={() => setRevealing(null)} />
      <SSHKeyInfoDialog account={showingKey} onClose={() => setShowingKey(null)} />
      <RotateTokenDialog
        account={rotating}
        onClose={() => setRotating(null)}
        onRotated={(_, note) => {
          setRotating(null);
          setNotice(note);
          reload();
        }}
      />
      <IdentityDialog
        account={editingIdentity}
        onClose={() => setEditingIdentity(null)}
        onSaved={() => {
          setEditingIdentity(null);
          reload();
        }}
      />
      <AddTokenDialog
        open={addingToken}
        initialProvider={tokenProvider}
        onClose={() => {
          setAddingToken(false);
          setTokenProvider(undefined);
        }}
        onSaved={(saved, note) => {
          setAddingToken(false);
          setNotice(note);
          reload();
          // The identity commits are written with is the next thing a new
          // account needs; ask for it straight away.
          setEditingIdentity(saved);
        }}
      />
      <ImportSSHKeysDialog
        open={importing}
        onClose={() => setImporting(false)}
        onImported={(saved, note) => {
          setImporting(false);
          setNotice(note || "Key imported.");
          reload();
          // Same as a pasted key: the public half is what goes into the provider.
          setShowingKey(saved);
        }}
      />
      <AddSSHKeyDialog
        open={adding}
        onClose={() => setAdding(false)}
        onSaved={(saved, note) => {
          setAdding(false);
          setNotice(note || "Key saved.");
          reload();
          // The public half is what the person needs next: it is what goes into
          // the provider's settings before the key can authenticate anything.
          setShowingKey(saved);
        }}
      />
    </div>
  );
}
