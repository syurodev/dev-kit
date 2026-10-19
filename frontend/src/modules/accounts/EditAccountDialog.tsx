import { useEffect, useId, useState } from "react";
import { Link2, Link2Off, RotateCw } from "lucide-react";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  createGitTokenAccount,
  isLocalAccount,
  linkAccountToken,
  renameAccount,
  unlinkAccountToken,
  verifyAccount,
  type Account,
} from "@/bridge/accounts";
import { canRename, defaultTokenLabel, linkableTokens, loginMismatch, takesLinkedToken } from "./accountGroups";
import { tokenNote, usernameRequired } from "./gitTokens";

const PROVIDER_NAME: Record<string, string> = { github: "GitHub", gitlab: "GitLab", bitbucket: "Bitbucket" };

/**
 * Editing an account that already exists: its name, and — for an SSH key — the
 * API token that goes with it.
 *
 * A key moves the code and a token reads the pull requests; to the person they
 * are one login, so this is where one is given the other instead of a second,
 * unrelated entry in the list. Two logins stay two accounts: only a token the
 * person chooses to attach is attached, and the dialog says when the provider
 * reports different people for the key and the token.
 */
export function EditAccountDialog({
  account,
  accounts,
  onClose,
  onChanged,
  onReplaceToken,
}: {
  /** Read from the loaded list, so it is current after every change made here. */
  account: Account | null;
  accounts: readonly Account[];
  onClose: () => void;
  /** Something changed on the server: the list is read again. */
  onChanged: (note?: string) => void;
  onReplaceToken: (token: Account) => void;
}) {
  const ids = useId();
  const [label, setLabel] = useState("");
  const [attach, setAttach] = useState("");
  const [token, setToken] = useState("");
  const [user, setUser] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const accountId = account?.id ?? "";
  useEffect(() => {
    // Nothing is kept from the last time: a token left in this form is a token
    // left in the render tree.
    setLabel(account?.label ?? "");
    setAttach("");
    setToken("");
    setUser("");
    setError("");
    // Only when another account is opened; a reload after a change keeps what is typed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accountId]);

  if (!account) {
    return <Dialog open={false} onOpenChange={() => {}} />;
  }

  const provider = PROVIDER_NAME[account.provider] ?? account.provider;
  const linked = account.linkedTokenId ? accounts.find((a) => a.id === account.linkedTokenId) : undefined;
  const free = takesLinkedToken(account) && !linked ? linkableTokens(account, accounts) : [];
  const servedKeys = (account.linkedKeyIds ?? [])
    .map((id) => accounts.find((a) => a.id === id))
    .filter((a): a is Account => !!a);
  const needsUser = usernameRequired(account.provider);
  const requests = account.provider === "gitlab" ? "merge requests" : "pull requests";

  async function run(action: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const rename = () =>
    run(async () => {
      await renameAccount(account.id, label.trim());
      onChanged("Renamed.");
    });

  const attachExisting = () =>
    run(async () => {
      await linkAccountToken(account.id, attach);
      setAttach("");
      onChanged("Token attached to this key.");
    });

  const detach = (keyId: string) =>
    run(async () => {
      await unlinkAccountToken(keyId);
      onChanged("Token detached. It is still in the list.");
    });

  const addAndAttach = () =>
    run(async () => {
      // The token is kept on this machine, like the key it goes with is by default:
      // a credential leaving it is a decision made in Add token, not here.
      const created = await createGitTokenAccount({
        provider: account.provider,
        label: defaultTokenLabel(account),
        token,
        user: user.trim(),
        syncAcrossDevices: false,
      });
      setToken("");
      try {
        await linkAccountToken(account.id, created.id);
      } catch (e) {
        onChanged();
        throw new Error(`The token was saved but could not be attached to this key: ${(e as Error).message}`);
      }
      let saved = created;
      let failure = "";
      if (!needsUser) {
        try {
          saved = await verifyAccount(created.id);
        } catch (e) {
          failure = (e as Error).message || "the provider could not be reached";
        }
      }
      onChanged(`${tokenNote(saved, failure)} It is attached to this key.`);
    });

  const renameDirty = canRename(account) && label.trim() !== "" && label.trim() !== account.label;
  const mismatch = linked ? loginMismatch(account, linked) : "";

  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent showCloseButton={!busy} className="max-h-[90vh] overflow-y-auto sm:max-w-140">
        <DialogHeader>
          <DialogTitle>Edit account · {account.label}</DialogTitle>
          <DialogDescription>
            {provider} {account.kind === "ssh-key" ? "SSH key" : account.kind === "token" ? "access token" : "account"}
            {account.subject ? ` · ${account.subject}` : ""}
          </DialogDescription>
        </DialogHeader>

        <FieldSet disabled={busy}>
          {canRename(account) ? (
            <Field>
              <FieldLabel htmlFor={`${ids}-label`}>Name</FieldLabel>
              <div className="flex gap-2">
                <Input
                  id={`${ids}-label`}
                  value={label}
                  maxLength={120}
                  onChange={(event) => setLabel(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" && renameDirty) {
                      event.preventDefault();
                      void rename();
                    }
                  }}
                />
                <Button type="button" variant="outline" disabled={busy || !renameDirty} onClick={() => void rename()}>
                  Save name
                </Button>
              </div>
              <FieldDescription>
                How it is listed. Remotes, choices and commit identity are bound to the account, not its name.
              </FieldDescription>
            </Field>
          ) : (
            <p className="text-caption-11 text-text-tertiary">
              This account is managed elsewhere, so it cannot be renamed here.
            </p>
          )}

          {takesLinkedToken(account) ? (
            <div className="flex flex-col gap-3 border-t border-border-subtle pt-3">
              <div>
                <div className="text-body-13-medium text-text-primary">API token</div>
                <p className="text-caption-11 text-text-tertiary">
                  The key signs git over SSH. A {provider} token lets DevKit read {requests} and their checks for this
                  login. Two different logins are two accounts: only attach a token of the same one.
                </p>
              </div>

              {linked ? (
                <div className="flex items-center gap-2 rounded-md border border-border-subtle px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-body-13-medium text-text-primary">
                      {linked.label}
                      {linked.subject ? <span className="font-normal text-text-secondary"> · {linked.subject}</span> : null}
                    </div>
                    <div className="truncate text-caption-11 text-text-tertiary">
                      {linked.state === "valid" ? "Valid" : linked.state.replace(/_/g, " ")}
                      {isLocalAccount(linked) ? " · this machine only" : ""}
                    </div>
                  </div>
                  <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => onReplaceToken(linked)}>
                    <RotateCw aria-hidden />
                    Replace token
                  </Button>
                  <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => void detach(account.id)}>
                    <Link2Off aria-hidden />
                    Detach
                  </Button>
                </div>
              ) : (
                <>
                  {free.length > 0 ? (
                    <Field>
                      <FieldLabel htmlFor={`${ids}-attach`}>Use a token you already have</FieldLabel>
                      <div className="flex gap-2">
                        <Select value={attach} onValueChange={setAttach} disabled={busy}>
                          <SelectTrigger id={`${ids}-attach`} className="w-full">
                            <SelectValue placeholder="Choose a token" />
                          </SelectTrigger>
                          <SelectContent>
                            {free.map((candidate) => (
                              <SelectItem key={candidate.id} value={candidate.id}>
                                {candidate.label}
                                {candidate.subject ? ` · ${candidate.subject}` : ""}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <Button type="button" variant="outline" disabled={busy || attach === ""} onClick={() => void attachExisting()}>
                          <Link2 aria-hidden />
                          Attach
                        </Button>
                      </div>
                    </Field>
                  ) : null}
                  <form
                    className="contents"
                    onSubmit={(event) => {
                      event.preventDefault();
                      if (token.trim() !== "") void addAndAttach();
                    }}
                  >
                    <Field>
                      <FieldLabel htmlFor={`${ids}-token`}>{free.length > 0 ? "Or add a new token" : "Add a token"}</FieldLabel>
                      <Input
                        id={`${ids}-token`}
                        type="password"
                        value={token}
                        autoComplete="off"
                        spellCheck={false}
                        placeholder={`Paste the ${provider} access token`}
                        onChange={(event) => setToken(event.target.value)}
                      />
                      <FieldDescription>
                        {account.provider === "gitlab"
                          ? "Create it on GitLab with the read_api scope (api to also let agents create and merge). "
                          : "A token that can read pull requests and checks. "}
                        It is checked with the provider, kept sealed, and stays on this machine.
                      </FieldDescription>
                    </Field>
                    <div>
                      <Button type="submit" variant="outline" disabled={busy || token.trim() === ""}>
                        <Link2 aria-hidden />
                        {busy ? "Saving…" : "Add and attach"}
                      </Button>
                    </div>
                  </form>
                </>
              )}
              {mismatch ? (
                <p className="text-caption-11 text-status-warning">
                  {mismatch} These look like different logins. If that is not what you meant, detach the token and attach
                  one of the key's own login.
                </p>
              ) : null}
            </div>
          ) : null}

          {account.kind === "token" && servedKeys.length > 0 ? (
            <div className="flex flex-col gap-2 border-t border-border-subtle pt-3">
              <div className="text-body-13-medium text-text-primary">Used with</div>
              {servedKeys.map((key) => (
                <div key={key.id} className="flex items-center gap-2 rounded-md border border-border-subtle px-3 py-2">
                  <div className="min-w-0 flex-1 truncate text-body-13 text-text-primary">
                    {key.label}
                    {key.subject ? <span className="text-text-secondary"> · {key.subject}</span> : null}
                  </div>
                  <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => void detach(key.id)}>
                    <Link2Off aria-hidden />
                    Detach
                  </Button>
                </div>
              ))}
              {servedKeys.map((key) => {
                const note = loginMismatch(key, account);
                return note ? (
                  <p key={key.id} className="text-caption-11 text-status-warning">
                    {note}
                  </p>
                ) : null;
              })}
            </div>
          ) : null}

          {error ? <FieldError>{error}</FieldError> : null}
        </FieldSet>

        <DialogFooter>
          <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
