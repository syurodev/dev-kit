import { useEffect, useId, useState } from "react";
import { KeyRound } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldDescription, FieldError, FieldLabel, FieldSet } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  accountProviders,
  createSSHKeyAccount,
  verifyAccount,
  type Account,
  type AccountProvider,
} from "@/bridge/accounts";

/**
 * Saving a private key as the account that authenticates git over SSH.
 *
 * The key never comes back out: the backend checks it opens (with the
 * passphrase, when it has one) before anything is saved, then keeps it sealed.
 * It stays on this machine unless the box below is ticked — a private key
 * leaving the machine is a decision the person makes, so it is never the default.
 */
export function AddSSHKeyDialog({
  open,
  onClose,
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  /** Called with the saved account, verified when the provider answered. */
  onSaved: (account: Account, note: string) => void;
}) {
  const ids = useId();
  // null until the list arrives: an empty list would read as "no providers".
  const [providers, setProviders] = useState<AccountProvider[] | null>(null);
  const [provider, setProvider] = useState("github");
  const [label, setLabel] = useState("");
  const [privateKey, setPrivateKey] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [sync, setSync] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    // Nothing is kept from the last time: a key left in this form is a key left
    // in the render tree.
    setLabel("");
    setPrivateKey("");
    setPassphrase("");
    setSync(false);
    setError("");
    setProviders(null);
    accountProviders()
      .then((all) => {
        const withKeys = all.filter((p) => p.SSHHost);
        setProviders(withKeys);
        setProvider((current) =>
          withKeys.some((p) => p.ID === current) ? current : (withKeys[0]?.ID ?? current),
        );
      })
      .catch((e: Error) => setError(e.message));
  }, [open]);

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const created = await createSSHKeyAccount({
        provider,
        label: label.trim(),
        privateKey,
        passphrase,
        syncAcrossDevices: sync,
      });
      // Ask the provider whose key this is. It is the proof the key is on an
      // account at all, and it fills in the login; a failure here does not undo
      // the save — the usual reason is that the public key is not added yet.
      let saved = created;
      let note = "";
      try {
        saved = await verifyAccount(created.id);
        note = saved.subject ? `Verified as ${saved.subject}.` : "";
      } catch (e) {
        note = `Saved, but ${(e as Error).message || "the provider could not be reached"}. Add the public key to the account, then verify it.`;
      }
      setPrivateKey("");
      setPassphrase("");
      onSaved(saved, note);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && !busy && onClose()}>
      <DialogContent showCloseButton={!busy} className="max-h-[90vh] overflow-y-auto sm:max-w-140">
        <form onSubmit={(event) => void save(event)} className="contents">
          <DialogHeader>
            <DialogTitle>Add an SSH key</DialogTitle>
            <DialogDescription>
              The key signs git over SSH for the repositories you choose it for. It is checked now and
              kept sealed; it cannot be shown again.
            </DialogDescription>
          </DialogHeader>

          <FieldSet disabled={busy}>
            <Field>
              <FieldLabel htmlFor={`${ids}-provider`}>Provider</FieldLabel>
              <Select value={provider} onValueChange={setProvider} disabled={busy || providers === null}>
                <SelectTrigger id={`${ids}-provider`} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(providers ?? []).map((p) => (
                    <SelectItem key={p.ID} value={p.ID}>
                      {p.Name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${ids}-label`}>Name</FieldLabel>
              <Input
                id={`${ids}-label`}
                value={label}
                placeholder="Work key"
                autoFocus
                onChange={(e) => setLabel(e.target.value)}
              />
              <FieldDescription>How this key is listed, and offered when a remote needs choosing.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${ids}-key`}>Private key</FieldLabel>
              <Textarea
                id={`${ids}-key`}
                value={privateKey}
                rows={6}
                spellCheck={false}
                autoComplete="off"
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                className="font-mono text-code-12"
                onChange={(e) => setPrivateKey(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${ids}-passphrase`}>Passphrase</FieldLabel>
              <Input
                id={`${ids}-passphrase`}
                type="password"
                value={passphrase}
                autoComplete="off"
                placeholder="Only if the key is encrypted"
                onChange={(e) => setPassphrase(e.target.value)}
              />
              <FieldDescription>Saved with the key, so a push does not stop to ask for it.</FieldDescription>
            </Field>
            <Field orientation="horizontal">
              <Checkbox
                id={`${ids}-sync`}
                checked={sync}
                onCheckedChange={(checked) => setSync(checked === true)}
              />
              <div className="flex flex-col gap-0.5">
                <FieldLabel htmlFor={`${ids}-sync`}>Sync to my other devices</FieldLabel>
                <FieldDescription>
                  {sync
                    ? "The key will be sealed and sync with the rest of your vault."
                    : "The key stays on this machine only."}
                </FieldDescription>
              </div>
            </Field>
            {error ? <FieldError>{error}</FieldError> : null}
          </FieldSet>

          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || label.trim() === "" || privateKey.trim() === ""}>
              <KeyRound aria-hidden />
              {busy ? "Saving…" : "Save key"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
