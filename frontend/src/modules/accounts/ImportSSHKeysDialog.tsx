import { useCallback, useEffect, useId, useMemo, useState } from "react";
import { FolderOpen, KeyRound, Search } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldLabel,
  FieldSet,
  FieldTitle,
} from "@/components/ui/field";
import { PendingStatus } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  accountProviders,
  chooseSSHKeyFile,
  describeSSHKeyFile,
  detectSSHProvider,
  discoverSSHKeys,
  importSSHKey,
  verifyAccount,
  type Account,
  type AccountProvider,
  type KeyFile,
} from "@/bridge/accounts";
import { aliasesToBind, defaultAliasChecks, savedAsNote, suggestLabel } from "./importKeys";

/**
 * Bringing a key that is already on this machine into an account.
 *
 * The backend reads the file the person picks, checks it opens, and keeps a
 * sealed copy; the file in ~/.ssh is never changed or removed. It stays on this
 * machine unless the box is ticked. Only a path the backend itself listed or
 * the native picker returned can be read, so nothing typed here can name a file.
 */
export function ImportSSHKeysDialog({
  open,
  onClose,
  onImported,
}: {
  open: boolean;
  onClose: () => void;
  onImported: (account: Account, note: string) => void;
}) {
  const ids = useId();
  const [providers, setProviders] = useState<AccountProvider[] | null>(null);
  // null until the scan arrives: an empty list would read as "no keys found".
  const [files, setFiles] = useState<KeyFile[] | null>(null);
  const [selected, setSelected] = useState("");
  const [step, setStep] = useState<"list" | "details">("list");

  const [provider, setProvider] = useState("github");
  const [label, setLabel] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [sync, setSync] = useState(false);
  const [checks, setChecks] = useState<Record<string, boolean>>({});
  const [detected, setDetected] = useState("");
  const [busy, setBusy] = useState(false);
  const [detecting, setDetecting] = useState(false);
  const [error, setError] = useState("");

  const withKeys = useMemo(() => (providers ?? []).filter((p) => p.SSHHost), [providers]);
  const file = files?.find((candidate) => candidate.path === selected) ?? null;
  const providerEntry = withKeys.find((p) => p.ID === provider);

  const scan = useCallback(() => {
    setFiles(null);
    discoverSSHKeys()
      .then(setFiles)
      .catch((e: Error) => {
        setFiles([]);
        setError(e.message);
      });
  }, []);

  useEffect(() => {
    if (!open) return;
    // Nothing is kept from the last time: a passphrase left in this form is a
    // passphrase left in the render tree.
    setStep("list");
    setSelected("");
    setLabel("");
    setPassphrase("");
    setSync(false);
    setChecks({});
    setDetected("");
    setError("");
    setProviders(null);
    accountProviders()
      .then(setProviders)
      .catch((e: Error) => setError(e.message));
    scan();
  }, [open, scan]);

  function choose(path: string) {
    setSelected(path);
    setError("");
  }

  async function chooseOther() {
    setError("");
    try {
      const path = await chooseSSHKeyFile();
      if (!path) return;
      const described = await describeSSHKeyFile(path, "");
      setFiles((current) => [...(current ?? []).filter((f) => f.path !== described.path), described]);
      setSelected(described.path);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  function next() {
    if (!file) return;
    const first = withKeys.find((p) => p.ID === provider) ?? withKeys[0];
    setProvider(first?.ID ?? provider);
    setLabel(suggestLabel(first?.Name ?? "SSH", "", file.name));
    setChecks(defaultAliasChecks(file.aliases, first));
    setDetected("");
    setError("");
    setStep("details");
  }

  function changeProvider(id: string) {
    setProvider(id);
    const entry = withKeys.find((p) => p.ID === id);
    if (file) {
      setChecks(defaultAliasChecks(file.aliases, entry));
      setLabel(suggestLabel(entry?.Name ?? "SSH", "", file.name));
    }
  }

  async function detect() {
    if (!file || detecting) return;
    setDetecting(true);
    setError("");
    setDetected("");
    try {
      const guesses = await detectSSHProvider(file.path, passphrase);
      if (guesses.length === 0) {
        setDetected("No provider knows this key yet. Add its public key to the account, then import it.");
        return;
      }
      const first = guesses[0];
      setProvider(first.provider);
      setLabel(suggestLabel(first.name, first.subject, file.name));
      setChecks(defaultAliasChecks(file.aliases, withKeys.find((p) => p.ID === first.provider)));
      setDetected(guesses.map((g) => `${g.name} knows it as ${g.subject || "an account"}`).join(" · "));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setDetecting(false);
    }
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (!file || busy) return;
    setBusy(true);
    setError("");
    try {
      const created = await importSSHKey({
        path: file.path,
        provider,
        label: label.trim(),
        passphrase,
        syncAcrossDevices: sync,
        aliases: aliasesToBind(file.aliases, checks),
      });
      // Ask the provider whose key this is: the proof it is on an account at
      // all, and the login. A failure does not undo the import.
      let saved = created;
      let note = "";
      try {
        saved = await verifyAccount(created.id);
        note = saved.subject ? `Imported and verified as ${saved.subject}.` : "Imported.";
      } catch (e) {
        note = `Imported, but ${(e as Error).message || "the provider could not be reached"}. Add the public key to the account, then verify it.`;
      }
      setPassphrase("");
      onImported(saved, note);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(value) => !value && !busy && onClose()}>
      <DialogContent showCloseButton={!busy} className="max-h-[90vh] overflow-y-auto sm:max-w-140">
        {step === "list" || !file ? (
          <>
            <DialogHeader>
              <DialogTitle>Import SSH keys</DialogTitle>
              <DialogDescription>
                Keys found in ~/.ssh. DevKit keeps a sealed copy of the one you choose; the file itself is not
                changed or removed.
              </DialogDescription>
            </DialogHeader>

            {files === null ? (
              <PendingStatus>Looking in ~/.ssh…</PendingStatus>
            ) : files.length === 0 ? (
              <FieldDescription>No private keys were found in ~/.ssh. Choose a key file instead.</FieldDescription>
            ) : (
              <RadioGroup value={selected} onValueChange={choose} aria-label="Keys found on this machine">
                {files.map((candidate) => (
                  <FieldLabel key={candidate.path} htmlFor={`${ids}-${candidate.path}`}>
                    <Field orientation="horizontal">
                      <FieldContent>
                        <FieldTitle>
                          {candidate.name}
                          {candidate.encrypted ? <Badge variant="outline">Encrypted</Badge> : null}
                        </FieldTitle>
                        <FieldDescription className="font-mono text-code-11 break-all">
                          {candidate.fingerprint
                            ? `${candidate.type} · ${candidate.fingerprint}`
                            : "Encrypted — needs its passphrase to be read"}
                        </FieldDescription>
                        {candidate.comment ? <FieldDescription>{candidate.comment}</FieldDescription> : null}
                        {candidate.aliases.length > 0 ? (
                          <FieldDescription>
                            In ~/.ssh/config for {candidate.aliases.map((a) => `${a.name} → ${a.hostName}`).join(", ")}
                          </FieldDescription>
                        ) : null}
                        {candidate.imported.length > 0 ? (
                          <FieldDescription>Already saved as {savedAsNote(candidate)}</FieldDescription>
                        ) : null}
                      </FieldContent>
                      <RadioGroupItem value={candidate.path} id={`${ids}-${candidate.path}`} />
                    </Field>
                  </FieldLabel>
                ))}
              </RadioGroup>
            )}

            {error ? <FieldError>{error}</FieldError> : null}

            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => void chooseOther()}>
                <FolderOpen aria-hidden />
                Choose another file…
              </Button>
              <div className="flex-1" />
              <Button type="button" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button type="button" disabled={!file || providers === null} onClick={next}>
                Next
              </Button>
            </DialogFooter>
          </>
        ) : (
          <form onSubmit={(event) => void save(event)} className="contents">
            <DialogHeader>
              <DialogTitle>Import {file.name}</DialogTitle>
              <DialogDescription className="font-mono text-code-11 break-all">
                {file.fingerprint ? `${file.type} · ${file.fingerprint}` : "Encrypted key"}
              </DialogDescription>
            </DialogHeader>

            <FieldSet disabled={busy}>
              {file.encrypted ? (
                <Field>
                  <FieldLabel htmlFor={`${ids}-passphrase`}>Passphrase</FieldLabel>
                  <Input
                    id={`${ids}-passphrase`}
                    type="password"
                    value={passphrase}
                    autoComplete="off"
                    autoFocus
                    onChange={(event) => setPassphrase(event.target.value)}
                  />
                  <FieldDescription>Checked now, and saved with the key so a push does not stop to ask.</FieldDescription>
                </Field>
              ) : null}

              <Field>
                <FieldLabel htmlFor={`${ids}-provider`}>Provider</FieldLabel>
                <div className="flex items-center gap-2">
                  <Select value={provider} onValueChange={changeProvider}>
                    <SelectTrigger id={`${ids}-provider`} className="flex-1">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {withKeys.map((p) => (
                        <SelectItem key={p.ID} value={p.ID}>
                          {p.Name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={detecting || (file.encrypted && passphrase === "")}
                    title="Connect to GitHub, GitLab and Bitbucket and ask which of them knows this key"
                    onClick={() => void detect()}
                  >
                    <Search aria-hidden />
                    {detecting ? "Asking…" : "Detect"}
                  </Button>
                </div>
                <FieldDescription>
                  {detected || "Detect connects to each provider to see which one has this key's public half."}
                </FieldDescription>
              </Field>

              <Field>
                <FieldLabel htmlFor={`${ids}-label`}>Name</FieldLabel>
                <Input id={`${ids}-label`} value={label} onChange={(event) => setLabel(event.target.value)} />
                <FieldDescription>How this key is listed, and offered when a remote needs choosing.</FieldDescription>
              </Field>

              {file.aliases.length > 0 ? (
                <Field>
                  <FieldTitle>ssh config aliases</FieldTitle>
                  {file.aliases.map((alias) => (
                    <Field key={alias.name} orientation="horizontal">
                      <Checkbox
                        id={`${ids}-alias-${alias.name}`}
                        checked={!!checks[alias.name]}
                        onCheckedChange={(checked) =>
                          setChecks((current) => ({ ...current, [alias.name]: checked === true }))
                        }
                      />
                      <div className="flex flex-col gap-0.5">
                        <FieldLabel htmlFor={`${ids}-alias-${alias.name}`}>
                          Use for remotes like git@{alias.name}:…
                        </FieldLabel>
                        <FieldDescription>
                          Your ssh config points {alias.name} at {alias.hostName} with this key, so these remotes
                          never need asking about.
                          {providerEntry && !providerEntry.Hosts?.includes(alias.hostName.toLowerCase())
                            ? ` That is not ${providerEntry.Name}'s host.`
                            : ""}
                        </FieldDescription>
                      </div>
                    </Field>
                  ))}
                </Field>
              ) : null}

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
              <Button type="button" variant="outline" disabled={busy} onClick={() => setStep("list")}>
                Back
              </Button>
              <Button
                type="submit"
                disabled={busy || label.trim() === "" || (file.encrypted && passphrase === "")}
              >
                <KeyRound aria-hidden />
                {busy ? "Importing…" : "Import key"}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
