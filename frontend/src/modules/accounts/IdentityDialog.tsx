import { useEffect, useId, useState } from "react";
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
import { setAccountIdentity, type Account } from "@/bridge/accounts";

/**
 * GitHub's address for people who keep their own private: commits made under
 * it are still credited to the account. It needs only the login, which a
 * verified account already has. Other providers have no address that can be
 * worked out from a login, so none is offered for them.
 */
function noreplySuggestion(account: Account): string {
  return account.provider === "github" && account.subject
    ? `${account.subject}@users.noreply.github.com`
    : "";
}

/**
 * Who commits are written as when this account is the one in use.
 *
 * A work account and a personal one differ in their email as much as in their
 * token, and a commit pushed under the wrong one cannot be taken back. Both
 * halves are asked for together: a name from one place and an email from
 * another is worse than either.
 */
export function IdentityDialog({
  account,
  onClose,
  onSaved,
}: {
  account: Account | null;
  onClose: () => void;
  onSaved: (account: Account) => void;
}) {
  const ids = useId();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    setName(account?.authorName ?? "");
    setEmail(account?.authorEmail ?? "");
    setError("");
  }, [account]);

  const suggestion = account ? noreplySuggestion(account) : "";

  function save(event: React.FormEvent) {
    event.preventDefault();
    if (!account || busy) return;
    setBusy(true);
    setError("");
    setAccountIdentity(account.id, name, email)
      .then(onSaved)
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false));
  }

  return (
    <Dialog open={!!account} onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent showCloseButton={!busy} className="sm:max-w-140">
        <form onSubmit={save} className="contents">
          <DialogHeader>
            <DialogTitle>Commit identity · {account?.label}</DialogTitle>
            <DialogDescription>
              Commits in repositories that use this account are written with this name and email. Leave both
              empty to use git's own configuration.
            </DialogDescription>
          </DialogHeader>
          <FieldSet disabled={busy}>
            <Field>
              <FieldLabel htmlFor={`${ids}-name`}>Name</FieldLabel>
              <Input
                id={`${ids}-name`}
                value={name}
                autoFocus
                autoComplete="off"
                placeholder="Ada Lovelace"
                onChange={(event) => setName(event.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${ids}-email`}>Email</FieldLabel>
              <Input
                id={`${ids}-email`}
                type="email"
                value={email}
                autoComplete="off"
                placeholder="ada@example.com"
                onChange={(event) => setEmail(event.target.value)}
              />
              <FieldDescription>
                {suggestion ? (
                  <>
                    Keeping your address private? Use{" "}
                    <button
                      type="button"
                      className="underline underline-offset-2 hover:text-text-primary"
                      onClick={() => setEmail(suggestion)}
                    >
                      {suggestion}
                    </button>
                    .
                  </>
                ) : (
                  "The address commits are credited to on the provider."
                )}
              </FieldDescription>
            </Field>
            {error ? <FieldError>{error}</FieldError> : null}
          </FieldSet>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy}>
              {busy ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
