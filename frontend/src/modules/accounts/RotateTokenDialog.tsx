import { useEffect, useId, useState } from "react";
import { RotateCw } from "lucide-react";
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
import { rotateGitToken, verifyAccount, type Account } from "@/bridge/accounts";
import { tokenNote, usernameRequired } from "./gitTokens";

/**
 * Replacing the token of an account that is already in use — it was revoked,
 * it expired, or it leaked.
 *
 * The account keeps its id, so every remote, remembered choice and commit
 * identity bound to it carries on with the new value and nothing needs editing.
 * What the provider last said about the old token is cleared, and the new one is
 * verified straight away so the list shows who it belongs to.
 */
export function RotateTokenDialog({
  account,
  onClose,
  onRotated,
}: {
  account: Account | null;
  onClose: () => void;
  onRotated: (account: Account, note: string) => void;
}) {
  const ids = useId();
  const [token, setToken] = useState("");
  const [user, setUser] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    // Nothing is kept from the last time: a token left in this form is a token
    // left in the render tree.
    setToken("");
    setUser("");
    setError("");
  }, [account]);

  const needsUser = account ? usernameRequired(account.provider) : false;

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (!account || busy) return;
    setBusy(true);
    setError("");
    try {
      let rotated = await rotateGitToken(account.id, token, user.trim());
      let failure = "";
      // Bitbucket has no endpoint that says who a token is, so there is nothing
      // to ask it; the note says so instead of reporting a failure.
      if (!needsUser) {
        try {
          rotated = await verifyAccount(account.id);
        } catch (e) {
          failure = (e as Error).message || "the provider could not be reached";
        }
      }
      setToken("");
      onRotated(rotated, tokenNote(rotated, failure, true));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={!!account} onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent showCloseButton={!busy} className="sm:max-w-140">
        <form onSubmit={(event) => void save(event)} className="contents">
          <DialogHeader>
            <DialogTitle>Replace token · {account?.label}</DialogTitle>
            <DialogDescription>
              The remotes, choices and commit identity bound to this account keep working with the new token.
              The old one stops being used as soon as this is saved.
            </DialogDescription>
          </DialogHeader>
          <FieldSet disabled={busy}>
            <Field>
              <FieldLabel htmlFor={`${ids}-token`}>New token</FieldLabel>
              <Input
                id={`${ids}-token`}
                type="password"
                value={token}
                autoComplete="off"
                spellCheck={false}
                autoFocus
                placeholder={needsUser ? "Paste the new app password" : "Paste the new access token"}
                onChange={(event) => setToken(event.target.value)}
              />
              <FieldDescription>
                It is checked with the provider after it is saved, and kept sealed like the old one.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${ids}-user`}>Username (optional)</FieldLabel>
              <Input
                id={`${ids}-user`}
                value={user}
                autoComplete="off"
                placeholder="Keep the current one"
                onChange={(event) => setUser(event.target.value)}
              />
              <FieldDescription>Leave empty to keep the username this account already has.</FieldDescription>
            </Field>
            {error ? <FieldError>{error}</FieldError> : null}
          </FieldSet>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || token.trim() === ""}>
              <RotateCw aria-hidden />
              {busy ? "Replacing…" : "Replace token"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
