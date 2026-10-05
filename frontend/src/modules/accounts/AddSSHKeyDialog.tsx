import { useEffect, useState } from "react";
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
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
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
        if (withKeys.length > 0 && !withKeys.some((p) => p.ID === provider)) {
          setProvider(withKeys[0].ID);
        }
      })
      .catch((e: Error) => setError(e.message));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  async function save() {
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
      <DialogContent className="sm:max-w-130" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>Add an SSH key</DialogTitle>
          <DialogDescription>
            The key signs git over SSH for the repositories you choose it for. It is
            checked now and kept sealed; it cannot be shown again.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <label className="space-y-1.5">
              <span className="text-label-12 text-text-primary">Provider</span>
              <NativeSelect
                className="w-full"
                value={provider}
                disabled={busy || providers === null}
                onChange={(e) => setProvider(e.target.value)}
              >
                {(providers ?? []).map((p) => (
                  <option key={p.ID} value={p.ID}>
                    {p.Name}
                  </option>
                ))}
              </NativeSelect>
            </label>
            <label className="space-y-1.5">
              <span className="text-label-12 text-text-primary">Name</span>
              <Input
                value={label}
                disabled={busy}
                placeholder="Work key"
                onChange={(e) => setLabel(e.target.value)}
              />
            </label>
          </div>
          <label className="block space-y-1.5">
            <span className="text-label-12 text-text-primary">Private key</span>
            <Textarea
              value={privateKey}
              disabled={busy}
              rows={6}
              spellCheck={false}
              autoComplete="off"
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
              className="font-mono text-code-12"
              onChange={(e) => setPrivateKey(e.target.value)}
            />
          </label>
          <label className="block space-y-1.5">
            <span className="text-label-12 text-text-primary">Passphrase</span>
            <Input
              type="password"
              value={passphrase}
              disabled={busy}
              autoComplete="off"
              placeholder="Only if the key is encrypted"
              onChange={(e) => setPassphrase(e.target.value)}
            />
            <span className="text-code-11 text-text-tertiary">
              Saved with the key, so a push does not stop to ask for it.
            </span>
          </label>
          <Checkbox
            checked={sync}
            disabled={busy}
            onCheckedChange={(checked) => setSync(checked === true)}
            label="Sync to my other devices"
          />
          <p className="text-code-11 text-text-tertiary">
            {sync
              ? "The key will be sealed and sync with the rest of your vault."
              : "The key stays on this machine only."}
          </p>
        </div>

        {error ? (
          <p className="text-code-11 text-status-danger" role="alert">
            {error}
          </p>
        ) : null}

        <DialogFooter className="sm:justify-end">
          <Button variant="outline" size="sm" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button
            size="sm"
            disabled={busy || label.trim() === "" || privateKey.trim() === ""}
            onClick={() => void save()}
          >
            <KeyRound className="size-4" aria-hidden />
            Save key
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
