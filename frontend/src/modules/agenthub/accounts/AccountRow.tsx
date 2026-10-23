import { useState, type ReactNode } from "react";
import { Check, ChevronDown, ChevronUp, CircleDashed, Pencil, RefreshCw, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { ACCOUNT_LABEL_MAX, accountNotes, readingText, usageRows } from "./accountModel";
import { TONE_TEXT, UsageMeter } from "./UsageMeter";

export type AccountRowProps = {
  account: AgentAccount;
  /** Epoch milliseconds, passed in so the row reads the same in a test and in the gallery. */
  now: number;
  /** Where the account is in the order the rules try them, and how many there are. */
  index: number;
  count: number;
  /** A change to this account is in flight. */
  busy: boolean;
  /** Why the last Refresh usage changed nothing, when it did not. */
  refreshNote?: string;
  /** The sign-in controls, drawn while the account is signed out. */
  signIn: ReactNode;
  /** The Remove confirmation is open for this account. */
  removing: boolean;
  /** Why the last change failed; shown inside the Remove confirmation while it is open. */
  error: string;
  onMove: (delta: -1 | 1) => void;
  onRefreshUsage: () => void;
  onRefreshStatus: () => void;
  onSetShared: (shared: boolean) => void;
  /** Resolves true when the name was stored, so the form can close. */
  onRename: (label: string) => Promise<boolean>;
  onAskRemove: () => void;
  onCancelRemove: () => void;
  /** Resolves true when the account was removed. */
  onConfirmRemove: () => Promise<boolean>;
};

function RenameForm({
  initial,
  busy,
  onSave,
  onCancel,
}: {
  initial: string;
  busy: boolean;
  onSave: (label: string) => Promise<boolean>;
  onCancel: () => void;
}) {
  const [text, setText] = useState(initial);
  const [problem, setProblem] = useState("");
  async function submit() {
    const name = text.trim();
    if (!name) {
      setProblem("Give the account a name.");
      return;
    }
    if (await onSave(name)) onCancel();
  }
  return (
    <form
      aria-label={`Rename ${initial}`}
      className="flex items-end gap-2"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <div className="w-60">
        <Input
          label="Name"
          value={text}
          maxLength={ACCOUNT_LABEL_MAX}
          error={problem || undefined}
          onChange={(event) => {
            setText(event.target.value);
            setProblem("");
          }}
        />
      </div>
      <Button type="submit" size="lg" disabled={busy || text.trim() === initial}>
        Save
      </Button>
      <Button type="button" variant="ghost" size="lg" disabled={busy} onClick={onCancel}>
        Cancel
      </Button>
    </form>
  );
}

/**
 * One account: who it is, whether it is signed in, how much of each window it
 * has used and how old that reading is, and what can be done to it. Everything
 * the account can say about itself is here; what the rules do with it is the
 * policy editor's.
 */
export function AccountRow({
  account,
  now,
  index,
  count,
  busy,
  refreshNote,
  signIn,
  removing,
  error,
  onMove,
  onRefreshUsage,
  onRefreshStatus,
  onSetShared,
  onRename,
  onAskRemove,
  onCancelRemove,
  onConfirmRemove,
}: AccountRowProps) {
  const [renaming, setRenaming] = useState(false);
  const notes = account.signedIn ? accountNotes(account, now) : [];
  const usage = account.signedIn ? usageRows(account, now) : [];
  return (
    <li
      data-testid="agent-account-row"
      data-account={account.id}
      data-signed-in={account.signedIn}
      className="flex flex-col gap-2.5 rounded-control border border-border-subtle bg-surface-raised px-3.5 py-2.5"
    >
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-body-13 font-medium text-text-primary">{account.label}</span>
        {account.builtin ? <Badge variant="outline">Built in</Badge> : null}
        {account.shared ? <Badge variant="secondary">Shared</Badge> : null}
        {count > 1 ? (
          <>
            <ActionTooltip label="Move up" wrap>
              <Button type="button" variant="ghost" size="icon-xs" disabled={busy || index === 0} onClick={() => onMove(-1)}>
                <ChevronUp />
              </Button>
            </ActionTooltip>
            <ActionTooltip label="Move down" wrap>
              <Button type="button" variant="ghost" size="icon-xs" disabled={busy || index === count - 1} onClick={() => onMove(1)}>
                <ChevronDown />
              </Button>
            </ActionTooltip>
          </>
        ) : null}
        {account.signedIn ? (
          <ActionTooltip label="Refresh usage" wrap>
            <Button type="button" variant="ghost" size="icon-xs" disabled={busy} onClick={onRefreshUsage}>
              <RefreshCw />
            </Button>
          </ActionTooltip>
        ) : null}
        {!account.builtin ? (
          <>
            <ActionTooltip label="Rename" wrap>
              <Button type="button" variant="ghost" size="icon-xs" disabled={busy} onClick={() => setRenaming(true)}>
                <Pencil />
              </Button>
            </ActionTooltip>
            <ActionTooltip label="Remove" wrap>
              <Button type="button" variant="ghost" size="icon-xs" disabled={busy} onClick={onAskRemove}>
                <Trash2 />
              </Button>
            </ActionTooltip>
          </>
        ) : null}
      </div>

      {renaming ? <RenameForm initial={account.label} busy={busy} onSave={onRename} onCancel={() => setRenaming(false)} /> : null}

      <div className="flex flex-wrap items-center gap-2">
        <span data-testid="agent-account-status" className="flex items-center gap-1.5 text-body-13 text-text-primary">
          {account.signedIn ? (
            <Check className="size-4 shrink-0 text-status-success" />
          ) : (
            <CircleDashed className="size-4 shrink-0 text-text-tertiary" />
          )}
          {account.signedIn ? "Signed in" : "Not signed in"}
        </span>
        {!account.signedIn ? (
          <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onRefreshStatus}>
            Check again
          </Button>
        ) : null}
      </div>

      {notes.map((note) => (
        <p key={note.key} data-testid="agent-account-note" className={cn("text-body-12", note.tone ? TONE_TEXT[note.tone] : "text-text-secondary")}>
          {note.text}
        </p>
      ))}

      {usage.length > 0 ? (
        <div className="flex flex-col gap-2.5">
          {usage.map((row) => (
            <UsageMeter key={row.key} row={row} />
          ))}
        </div>
      ) : null}

      {account.signedIn ? (
        <p data-testid="agent-account-reading" className="text-caption-11 text-text-tertiary">
          {readingText(account, now)}
          {account.shared ? " · a lower bound: other people use this login too" : ""}
        </p>
      ) : null}
      {refreshNote ? (
        <p className="text-caption-11 text-text-tertiary" role="status">
          {refreshNote}
        </p>
      ) : null}

      <label className="flex items-center gap-2.5 text-body-12 text-text-secondary">
        <span className="min-w-0 flex-1">Other people also use this login</span>
        <Switch
          aria-label={`Other people also use the ${account.label} login`}
          checked={account.shared}
          disabled={busy}
          onCheckedChange={onSetShared}
        />
      </label>

      {!account.signedIn ? signIn : null}

      <ConfirmDialog
        open={removing}
        onClose={onCancelRemove}
        title={`Remove ${account.label}?`}
        confirmLabel="Remove account"
        pendingLabel="Removing…"
        pending={busy}
        error={error || undefined}
        onConfirm={() => void onConfirmRemove()}
      >
        DevKit removes this account and its folder, including the login stored there. A session that is running as it must
        be closed first.
      </ConfirmDialog>
    </li>
  );
}
