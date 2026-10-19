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
import {
  accountProviders,
  createGitTokenAccount,
  verifyAccount,
  type Account,
  type AccountProvider,
} from "@/bridge/accounts";
import { gitHostProviders, tokenNote, usernameRequired } from "./gitTokens";

/**
 * Saving a personal access token as the account that authenticates git over HTTPS.
 *
 * The token is sealed and cannot be shown again without the Master Password. It
 * stays on this machine unless the box below is ticked — a token leaving the
 * machine is a decision the person makes, so it is never the default.
 */
export function AddTokenDialog({
  open,
  initialProvider,
  onClose,
  onSaved,
}: {
  open: boolean;
  /** The provider to start on when it hosts repositories ("gitlab"), else the form's own default. */
  initialProvider?: string;
  onClose: () => void;
  /** Called with the saved account, verified when the provider answered. */
  onSaved: (account: Account, note: string) => void;
}) {
  const ids = useId();
  // null until the list arrives: an empty list would read as "no providers".
  const [providers, setProviders] = useState<AccountProvider[] | null>(null);
  const [provider, setProvider] = useState("github");
  const [label, setLabel] = useState("");
  const [token, setToken] = useState("");
  const [user, setUser] = useState("");
  const [sync, setSync] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const needsUser = usernameRequired(provider);
  const providerName = providers?.find((p) => p.ID === provider)?.Name ?? "the provider";

  useEffect(() => {
    if (!open) return;
    // Nothing is kept from the last time: a token left in this form is a token
    // left in the render tree.
    setLabel("");
    setToken("");
    setUser("");
    setSync(false);
    setError("");
    setProviders(null);
    accountProviders()
      .then((all) => {
        const hosts = gitHostProviders(all);
        setProviders(hosts);
        setProvider((current) => {
          const wanted = initialProvider && hosts.some((p) => p.ID === initialProvider) ? initialProvider : current;
          return hosts.some((p) => p.ID === wanted) ? wanted : (hosts[0]?.ID ?? wanted);
        });
      })
      .catch((e: Error) => setError(e.message));
  }, [open, initialProvider]);

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const created = await createGitTokenAccount({
        provider,
        label: label.trim(),
        token,
        user: user.trim(),
        syncAcrossDevices: sync,
      });
      // Ask the provider whose token this is: the proof it works at all, and the
      // login. A failure here does not undo the save.
      let saved = created;
      let failure = "";
      // Bitbucket has no endpoint that says who a token is, so there is nothing
      // to ask it; the note says so instead of reporting a failure.
      if (!needsUser) {
        try {
          saved = await verifyAccount(created.id);
        } catch (e) {
          failure = (e as Error).message || "the provider could not be reached";
        }
      }
      setToken("");
      onSaved(saved, tokenNote(saved, failure));
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
            <DialogTitle>Add an access token</DialogTitle>
            <DialogDescription>
              For repositories cloned over HTTPS. The token is checked with the provider and kept sealed.
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
                placeholder="Work token"
                autoFocus
                onChange={(e) => setLabel(e.target.value)}
              />
              <FieldDescription>How this token is listed, and offered when a remote needs choosing.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${ids}-token`}>Token</FieldLabel>
              <Input
                id={`${ids}-token`}
                type="password"
                value={token}
                autoComplete="off"
                spellCheck={false}
                placeholder={needsUser ? "Paste the app password" : "Paste the access token"}
                onChange={(e) => setToken(e.target.value)}
              />
              <FieldDescription>
                {needsUser
                  ? "An app password with read and write access to repositories."
                  : "Needs to read and write repositories: the repo scope for a classic GitHub token, or Contents read and write for a fine-grained one; write_repository on GitLab."}
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${ids}-user`}>Username{needsUser ? "" : " (optional)"}</FieldLabel>
              <Input
                id={`${ids}-user`}
                value={user}
                autoComplete="off"
                placeholder={needsUser ? "Your Bitbucket username" : "Uses your login when empty"}
                onChange={(e) => setUser(e.target.value)}
              />
              <FieldDescription>
                {needsUser
                  ? `${providerName} refuses an app password without the username it belongs to.`
                  : `${providerName} does not look at it for a personal access token, but git asks for one.`}
              </FieldDescription>
            </Field>
            <Field orientation="horizontal">
              <Checkbox id={`${ids}-sync`} checked={sync} onCheckedChange={(checked) => setSync(checked === true)} />
              <div className="flex flex-col gap-0.5">
                <FieldLabel htmlFor={`${ids}-sync`}>Sync to my other devices</FieldLabel>
                <FieldDescription>
                  {sync
                    ? "The token will be sealed and sync with the rest of your vault."
                    : "The token stays on this machine only."}
                </FieldDescription>
              </div>
            </Field>
            {error ? <FieldError>{error}</FieldError> : null}
          </FieldSet>

          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={busy || label.trim() === "" || token.trim() === "" || (needsUser && user.trim() === "")}
            >
              <KeyRound aria-hidden />
              {busy ? "Saving…" : "Save token"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
