import { useEffect, useState } from "react";
import { Copy } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { copyText } from "@/bridge/clipboard";
import { sshKeyInfo, type Account, type SSHKeyInfo } from "@/bridge/accounts";

/**
 * The public half of a key account: what to paste into the provider's SSH key
 * settings, and the fingerprint to match against what it lists. Nothing here is
 * private, which is why it needs no Master Password.
 */
export function SSHKeyInfoDialog({
  account,
  onClose,
}: {
  account: Account | null;
  onClose: () => void;
}) {
  const [info, setInfo] = useState<SSHKeyInfo | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    setInfo(null);
    setError("");
    if (!account) return;
    sshKeyInfo(account.id)
      .then(setInfo)
      .catch((e: Error) => setError(e.message));
  }, [account]);

  return (
    <Dialog open={!!account} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-130" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{account?.label}</DialogTitle>
          <DialogDescription>
            Add this public key to your {account?.provider} account's SSH keys.
          </DialogDescription>
        </DialogHeader>
        {info ? (
          <div className="space-y-3">
            <div className="space-y-1.5">
              <span className="text-label-12 text-text-primary">Fingerprint</span>
              <p className="rounded-control border border-border-subtle bg-surface-well px-3 py-2 font-mono text-code-12 break-all">
                {info.fingerprint}
              </p>
            </div>
            <div className="space-y-1.5">
              <span className="text-label-12 text-text-primary">Public key</span>
              <p className="rounded-control border border-border-subtle bg-surface-well px-3 py-2 font-mono text-code-12 break-all">
                {info.publicKey}
              </p>
            </div>
          </div>
        ) : error ? (
          <p className="text-code-11 text-status-danger" role="alert">
            {error}
          </p>
        ) : (
          <p className="text-body-13 text-text-secondary">Loading…</p>
        )}
        <DialogFooter className="sm:justify-end">
          <Button variant="outline" size="sm" onClick={onClose}>
            Done
          </Button>
          {info ? (
            <Button size="sm" onClick={() => void copyText(info.publicKey)}>
              <Copy className="size-4" aria-hidden />
              Copy public key
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
