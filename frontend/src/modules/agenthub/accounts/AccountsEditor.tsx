import { useState, type ReactNode } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { AccountRow } from "./AccountRow";
import { AddAccountForm } from "./AddAccountForm";
import { builtinAccountHelp, builtinAccountHiddenHelp, orderedAccounts } from "./accountModel";

export type AccountsEditorProps = {
  vendor: string;
  vendorName: string;
  /** Undefined until the first read answers. */
  accounts: readonly AgentAccount[] | undefined;
  /** The order the person saved; ids of removed accounts may be in it. */
  order: readonly string[];
  /** Why the list could not be read. */
  loadError: string;
  /** The id of the account a change is in flight for, or "add" while one is being added; "" when idle. */
  busyId: string;
  /** Why the last change failed. */
  error: string;
  /** Why the last Refresh usage of an account changed nothing, by account id. */
  refreshNotes: Readonly<Record<string, string>>;
  now: number;
  /** The sign-in controls for a signed-out account. */
  renderSignIn: (account: AgentAccount) => ReactNode;
  onAdd: (label: string) => Promise<boolean>;
  onRename: (id: string, label: string) => Promise<boolean>;
  onRemove: (id: string) => Promise<boolean>;
  onSetShared: (id: string, shared: boolean) => void;
  onRefreshUsage: (id: string) => void;
  onRefreshStatus: (id: string) => void;
  onMove: (id: string, delta: -1 | 1) => void;
  /** Called when a removal is asked for, so the last change's failure does not sit beside the new question. */
  onDismissError?: () => void;
  /** Opens with the Add account form already showing (the gallery draws it so). */
  initialAddOpen?: boolean;
};

/**
 * A vendor's accounts, in the order the switching rules try them. Presentational:
 * the container reads, changes and refreshes, so the gallery can draw every state
 * (`?gallery#agent-accounts`).
 */
export function AccountsEditor({
  vendor,
  vendorName,
  accounts,
  order,
  loadError,
  busyId,
  error,
  refreshNotes,
  now,
  renderSignIn,
  onAdd,
  onRename,
  onRemove,
  onSetShared,
  onRefreshUsage,
  onRefreshStatus,
  onMove,
  onDismissError,
  initialAddOpen,
}: AccountsEditorProps) {
  const [removingAsked, setRemovingId] = useState("");
  const list = accounts ? orderedAccounts(accounts, order) : [];
  // A removal can fail after the account is gone (its folder was left behind): the
  // list then no longer has the row the confirmation lives in, and the page shows the sentence.
  const removingId = list.some((account) => account.id === removingAsked) ? removingAsked : "";
  return (
    <section className="flex flex-col gap-3" aria-label={`${vendorName} accounts`}>
      <h3 className="text-body-14-medium text-text-primary">Accounts</h3>
      <p className="text-body-12 text-text-tertiary">
        {list.some((account) => account.builtin)
          ? `A ${vendorName} session runs as one of these accounts. ${builtinAccountHelp(vendor)}`
          : builtinAccountHiddenHelp(vendor)}
        {list.length > 1 ? " When a session has to move, DevKit tries them from the top." : ""}
      </p>

      {accounts === undefined && !loadError ? <Skeleton className="h-24 w-full rounded-control" /> : null}
      {loadError ? (
        <p className="text-body-12 text-status-danger" role="alert">
          {loadError}
        </p>
      ) : null}
      {accounts !== undefined && list.length === 0 && !loadError ? (
        <p className="rounded-control border border-dashed border-border-subtle px-3.5 py-3 text-body-13 text-text-secondary">
          No accounts yet.
        </p>
      ) : null}

      {list.length > 0 ? (
        <ul className="flex flex-col gap-2" aria-label={`${vendorName} accounts`}>
          {list.map((account, index) => (
            <AccountRow
              key={account.id}
              account={account}
              now={now}
              index={index}
              count={list.length}
              busy={busyId === account.id}
              refreshNote={refreshNotes[account.id]}
              signIn={renderSignIn(account)}
              removing={removingId === account.id}
              error={error}
              onMove={(delta) => onMove(account.id, delta)}
              onRefreshUsage={() => onRefreshUsage(account.id)}
              onRefreshStatus={() => onRefreshStatus(account.id)}
              onSetShared={(shared) => onSetShared(account.id, shared)}
              onRename={(label) => onRename(account.id, label)}
              onAskRemove={() => {
                onDismissError?.();
                setRemovingId(account.id);
              }}
              onCancelRemove={() => setRemovingId("")}
              onConfirmRemove={async () => {
                const removed = await onRemove(account.id);
                if (removed) setRemovingId("");
                return removed;
              }}
            />
          ))}
        </ul>
      ) : null}

      {error && !removingId ? (
        <p className="text-body-12 text-status-danger" role="alert">
          {error}
        </p>
      ) : null}

      <AddAccountForm vendorName={vendorName} busy={busyId === "add"} onAdd={onAdd} initiallyOpen={initialAddOpen} />

      <p className="text-body-12 text-text-tertiary">
        Mark an account as shared when other people also use that login. What DevKit sees of its usage is then a lower
        bound, so DevKit asks the vendor itself before a turn once its last check is older than the re-check time below.
      </p>
    </section>
  );
}
