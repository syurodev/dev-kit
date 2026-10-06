import { useEffect, useId, useState } from "react";
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
import { Field, FieldDescription, FieldError, FieldLabel, FieldSet } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { PendingStatus } from "@/components/ui/feedback";
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
  const ids = useId();
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
      <DialogContent className="sm:max-w-140">
        <DialogHeader>
          <DialogTitle>{account?.label}</DialogTitle>
          <DialogDescription>
            Add this public key to your {account?.provider} account's SSH keys.
          </DialogDescription>
        </DialogHeader>
        {info ? (
          <FieldSet>
            <Field>
              <FieldLabel htmlFor={`${ids}-fingerprint`}>Fingerprint</FieldLabel>
              <Input id={`${ids}-fingerprint`} readOnly value={info.fingerprint} className="font-mono text-code-12" />
              <FieldDescription>Matches what the provider lists next to the key once it is added.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${ids}-public`}>Public key</FieldLabel>
              <Textarea
                id={`${ids}-public`}
                readOnly
                rows={3}
                value={info.publicKey}
                className="font-mono text-code-12 break-all"
              />
            </Field>
          </FieldSet>
        ) : error ? (
          <FieldError>{error}</FieldError>
        ) : (
          <PendingStatus>Reading the key…</PendingStatus>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Done
          </Button>
          {info ? (
            <Button onClick={() => void copyText(info.publicKey)}>
              <Copy aria-hidden />
              Copy public key
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
