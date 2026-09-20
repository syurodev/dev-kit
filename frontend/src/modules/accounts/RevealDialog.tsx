import { useEffect, useState } from "react";
import { Copy, Eye } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { copyText } from "@/bridge/clipboard";
import {
  revealAccount,
  revealRequiresPassword,
  type Account,
  type RevealedCredential,
} from "@/bridge/accounts";

/**
 * Showing a credential to the person it belongs to.
 *
 * This is the only place a value comes back across the bridge, and it exists
 * because the alternative is worse: somebody who cannot read back the token
 * they saved keeps a second copy of it in a note DevKit never sealed.
 *
 * Whether the Master Password is asked for is the backend's decision, not this
 * dialog's — the preference is read here only to know which field to draw. A
 * dialog that decided for itself would be a check an attacker could skip by
 * calling the method directly.
 */
export function RevealDialog({
  account,
  onClose,
}: {
  account: Account | null;
  onClose: () => void;
}) {
  const [needsPassword, setNeedsPassword] = useState<boolean | null>(null);
  const [password, setPassword] = useState("");
  const [revealed, setRevealed] = useState<RevealedCredential | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!account) return;
    // Nothing is carried over from the last account: a value left on screen
    // after the dialog moved on is the one way this leaks by itself.
    setPassword("");
    setRevealed(null);
    setError("");
    setNeedsPassword(null);
    revealRequiresPassword()
      .then(setNeedsPassword)
      .catch(() => setNeedsPassword(true));
  }, [account]);

  function reveal() {
    if (!account) return;
    setBusy(true);
    setError("");
    revealAccount(account.id, password)
      .then((credential) => {
        setRevealed(credential);
        // The password has done its job; keeping it in state would leave it in
        // the render tree for as long as the dialog is open.
        setPassword("");
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false));
  }

  return (
    <Dialog open={!!account} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[520px]" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>Show the value for {account?.label}</DialogTitle>
        </DialogHeader>

        {revealed ? (
          <div className="space-y-2">
            {revealed.user ? (
              <div className="space-y-1.5">
                <span className="text-label-12 text-text-primary">User</span>
                <p className="rounded-control border border-border-subtle bg-surface-well px-3 py-2 font-mono text-code-12 break-all">
                  {revealed.user}
                </p>
              </div>
            ) : null}
            <div className="space-y-1.5">
              <span className="text-label-12 text-text-primary">Secret</span>
              <p className="rounded-control border border-border-subtle bg-surface-well px-3 py-2 font-mono text-code-12 break-all">
                {revealed.secret}
              </p>
            </div>
            <p className="text-code-11 text-text-tertiary">
              This was written to the audit log as a reveal — the provider and the
              subject, never the value.
            </p>
          </div>
        ) : needsPassword === null ? (
          <p className="text-body-13 text-text-secondary">Loading…</p>
        ) : needsPassword ? (
          <div className="space-y-1.5">
            <label className="text-label-12 text-text-primary" htmlFor="reveal-password">
              Master Password
            </label>
            <Input
              id="reveal-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && !busy && reveal()}
              autoFocus
            />
            <p className="text-code-11 text-text-tertiary">
              Checking this does not lock or unlock anything. You can turn the question
              off in Settings.
            </p>
          </div>
        ) : (
          <p className="text-body-13 text-text-secondary">
            This shows the stored value on screen. Asking for the Master Password first
            is turned off in Settings.
          </p>
        )}

        {error ? (
          <p className="text-code-11 text-status-danger" role="alert">
            {error}
          </p>
        ) : null}

        <DialogFooter className="sm:justify-end">
          <Button variant="outline" size="sm" onClick={onClose}>
            {revealed ? "Done" : "Cancel"}
          </Button>
          {revealed ? (
            <Button size="sm" onClick={() => void copyText(revealed.secret)}>
              <Copy className="size-4" aria-hidden />
              Copy secret
            </Button>
          ) : (
            <Button
              size="sm"
              disabled={busy || needsPassword === null || (needsPassword && password === "")}
              onClick={reveal}
            >
              <Eye className="size-4" aria-hidden />
              Show
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
