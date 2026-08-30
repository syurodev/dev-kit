import { KeyRound, ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { requestVaultUnlock } from "@/workbench/vaultUnlockRequest";

/**
 * Content-area states from Figma `15 Database`. Copy is the design's, not a
 * paraphrase: each one exists because a bare driver error left the reader with
 * nothing to do next.
 */
function ContentState({
  icon,
  title,
  body,
  detail,
  actions,
}: {
  icon: React.ReactNode;
  title: string;
  body: string;
  detail?: string;
  actions: React.ReactNode;
}) {
  return (
    <div className="flex-1 flex flex-col items-center justify-center text-center p-8 gap-3">
      <div className="w-12 h-12 rounded-xl bg-surface-raised border border-border flex items-center justify-center">
        {icon}
      </div>
      <h3 className="text-sm font-semibold text-text-primary">{title}</h3>
      <p className="text-xs max-w-md text-text-secondary leading-relaxed">{body}</p>
      {detail && (
        <p className="text-[11px] font-mono text-text-tertiary bg-surface-raised border border-border rounded px-2 py-1">
          {detail}
        </p>
      )}
      <div className="flex items-center gap-2 pt-1">{actions}</div>
    </div>
  );
}

/** Figma `DB / Content — Vault locked` (521:2075). */
export function VaultLockedState() {
  return (
    <ContentState
      icon={<KeyRound className="w-6 h-6 text-status-warning" />}
      title="Unlock the vault to use this connection"
      body="Connection passwords and saved SQL are sealed with your vault key. DevKit will not show an empty list instead — nothing is readable until the vault is open."
      actions={
        <Button size="sm" onClick={requestVaultUnlock}>
          Unlock vault
        </Button>
      }
    />
  );
}

/** Figma `DB / Content — TLS refused` (521:2090). */
export function TLSRefusedState({
  target,
  onEditConnection,
}: {
  target: string;
  onEditConnection: () => void;
}) {
  return (
    <ContentState
      icon={<ShieldAlert className="w-6 h-6 text-status-danger" />}
      title="Refused: insecure TLS on a remote host"
      body="This profile points at a remote host with sslmode=disable. DevKit only allows unverified TLS on loopback, so the connection was never attempted."
      detail={`ErrInsecureTLSForRemoteHost · ${target}`}
      actions={
        <Button variant="outline" size="sm" onClick={onEditConnection}>
          Edit connection
        </Button>
      }
    />
  );
}
