import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { NativeSelect } from "@/components/ui/native-select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  createSSHProfile,
  updateSSHProfile,
  type SSHProfile,
} from "@/bridge/ssh";
import { InlineFeedback, PendingStatus } from "@/components/ui/feedback";
import { validateConnection } from "./validation";
import { errorMessage } from "@/bridge/errors";

export function Field({
  label,
  children,
  error,
  errorId,
}: {
  label: string;
  children: React.ReactNode;
  error?: string;
  errorId?: string;
}) {
  return (
    <label className="flex min-w-0 flex-col gap-1.5 text-body-12 text-text-secondary">
      <span>{label}</span>
      {children}
      {error && <InlineFeedback id={errorId}>{error}</InlineFeedback>}
    </label>
  );
}

export function ConnectionDialog({
  profile,
  envs,
  onClose,
  onSaved,
}: {
  profile?: SSHProfile;
  envs: string[];
  onClose: () => void;
  onSaved: (profile: SSHProfile) => Promise<void>;
}) {
  const [form, setForm] = useState({
    name: profile?.name ?? "",
    host: profile?.host ?? "",
    port: String(profile?.port ?? 22),
    username: profile?.username ?? "",
    env: profile?.env ?? (envs.includes("dev") ? "dev" : (envs[0] ?? "")),
    authMethod: profile?.authMethod ?? "key",
    secret: "",
    keyPassphrase: "",
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [fieldErrors, setFieldErrors] = useState<ReturnType<typeof validateConnection>>({});
  const set = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) => {
    setForm((s) => ({ ...s, [key]: value }));
    setFieldErrors((errors) => ({ ...errors, [key]: undefined }));
  };
  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    setError("");
    const errors = validateConnection(form, Boolean(profile));
    setFieldErrors(errors);
    if (Object.keys(errors).length) {
      const first = Object.keys(errors)[0];
      (event.currentTarget.elements.namedItem(first) as HTMLElement | null)?.focus();
      return;
    }
    setBusy(true);
    setError("");
    try {
      const saved = profile
        ? await updateSSHProfile(profile.id, {
            name: form.name.trim(),
            host: form.host.trim(),
            port: Number(form.port),
            username: form.username.trim(),
            env: form.env,
          })
        : await createSSHProfile({ ...form, port: Number(form.port), name: form.name.trim(), host: form.host.trim(), username: form.username.trim() });
      await onSaved(saved);
      onClose();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogContent showCloseButton={!busy} className="sm:max-w-xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {profile ? "Edit connection" : "New SSH connection"}
          </DialogTitle>
          <DialogDescription>
            {profile
              ? "Changing connection settings closes its terminals and tunnels. A new host or port needs host-key approval again."
              : "Save a connection, then verify its host key before connecting."}
          </DialogDescription>
        </DialogHeader>
        <form noValidate aria-busy={busy} onSubmit={(e) => void save(e)} className="flex flex-col gap-4">
          <fieldset disabled={busy} className="grid grid-cols-2 gap-3">
            <legend className="mb-2 text-label-12 text-text-primary">
              Connection
            </legend>
            <Field label="Name" error={fieldErrors.name} errorId="ssh-name-error">
              <Input
                required
                autoFocus
                aria-label="ssh-name"
                name="name"
                aria-invalid={Boolean(fieldErrors.name)}
                aria-describedby={fieldErrors.name ? "ssh-name-error" : undefined}
                value={form.name}
                onChange={(e) => set("name", e.target.value)}
                placeholder="e.g. API staging"
              />
            </Field>
            <Field label="Environment" error={fieldErrors.env} errorId="ssh-env-error">
              <NativeSelect
                className="w-full"
                aria-label="ssh-env"
                name="env"
                aria-invalid={Boolean(fieldErrors.env)}
                aria-describedby={fieldErrors.env ? "ssh-env-error" : undefined}
                value={form.env}
                onChange={(e) => set("env", e.target.value)}
              >
                {[...new Set([...envs, ...(form.env ? [form.env] : [])])].map(
                  (env) => (
                    <option key={env}>{env}</option>
                  ),
                )}
              </NativeSelect>
            </Field>
            <Field label="Host" error={fieldErrors.host} errorId="ssh-host-error">
              <Input
                required
                aria-label="ssh-host"
                name="host"
                aria-invalid={Boolean(fieldErrors.host)}
                aria-describedby={fieldErrors.host ? "ssh-host-error" : undefined}
                value={form.host}
                onChange={(e) => set("host", e.target.value)}
                placeholder="server.example.com"
              />
            </Field>
            <Field label="Port" error={fieldErrors.port} errorId="ssh-port-error">
              <Input
                required
                aria-label="ssh-port"
                name="port"
                aria-invalid={Boolean(fieldErrors.port)}
                aria-describedby={fieldErrors.port ? "ssh-port-error" : undefined}
                type="number"
                min={1}
                max={65535}
                value={form.port}
                onChange={(e) => set("port", e.target.value)}
              />
            </Field>
            <Field label="Username" error={fieldErrors.username} errorId="ssh-username-error">
              <Input
                required
                aria-label="ssh-username"
                name="username"
                aria-invalid={Boolean(fieldErrors.username)}
                aria-describedby={fieldErrors.username ? "ssh-username-error" : undefined}
                value={form.username}
                onChange={(e) => set("username", e.target.value)}
                placeholder="deploy"
              />
            </Field>
            <Field label="Authentication">
              <NativeSelect
                className="w-full"
                disabled={Boolean(profile)}
                aria-label="ssh-auth"
                value={form.authMethod}
                onChange={(e) => {
                  set("authMethod", e.target.value);
                  set("secret", "");
                  set("keyPassphrase", "");
                }}
              >
                <option value="key">Private key</option>
                <option value="password">Password</option>
              </NativeSelect>
            </Field>
          </fieldset>
          {!profile && (
            <fieldset disabled={busy} className="flex flex-col gap-3">
              <legend className="mb-2 text-label-12 text-text-primary">
                Authentication
              </legend>
              <Field
                error={fieldErrors.secret}
                errorId="ssh-secret-error"
                label={
                  form.authMethod === "key"
                    ? "Private key (PEM / OpenSSH)"
                    : "Password"
                }
              >
                {form.authMethod === "key" ? (
                  <Textarea
                    required
                    aria-label="ssh-secret"
                    name="secret"
                    aria-invalid={Boolean(fieldErrors.secret)}
                    aria-describedby={fieldErrors.secret ? "ssh-secret-error" : undefined}
                    autoComplete="off"
                    spellCheck={false}
                    className="font-mono max-h-48"
                    value={form.secret}
                    onChange={(e) => set("secret", e.target.value)}
                    placeholder="Paste your private key"
                  />
                ) : (
                  <Input
                    required
                    aria-label="ssh-secret"
                    name="secret"
                    aria-invalid={Boolean(fieldErrors.secret)}
                    aria-describedby={fieldErrors.secret ? "ssh-secret-error" : undefined}
                    type="password"
                    autoComplete="new-password"
                    value={form.secret}
                    onChange={(e) => set("secret", e.target.value)}
                  />
                )}
              </Field>
              {form.authMethod === "key" && (
                <Field label="Key passphrase (if encrypted)">
                  <Input
                    aria-label="ssh-key-passphrase"
                    type="password"
                    autoComplete="new-password"
                    value={form.keyPassphrase}
                    onChange={(e) => set("keyPassphrase", e.target.value)}
                  />
                </Field>
              )}
              <p className="text-caption-11 text-text-tertiary">
                Credentials are encrypted locally. A saved key passphrase also
                allows background database tunnels.
              </p>
            </fieldset>
          )}
          {profile && (
            <p className="text-body-12 text-text-secondary">
              Credentials stay unchanged. Manage the saved password or key in
              Accounts.
            </p>
          )}
          {error && <InlineFeedback>{error}</InlineFeedback>}
          {busy && <PendingStatus>Saving connection…</PendingStatus>}
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              disabled={busy}
              onClick={onClose}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !form.env}>
              {busy ? "Saving…" : "Save connection"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
