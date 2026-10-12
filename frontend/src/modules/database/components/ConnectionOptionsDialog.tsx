import { useId, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { NativeSelect } from "@/components/ui/native-select";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldLabel,
  FieldSet,
} from "@/components/ui/field";
import { FeedbackBanner, InlineFeedback } from "@/components/ui/feedback";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import {
  updateProfileOptions,
  type ConnectionProfile,
} from "@/bridge/database";
import { errorMessage } from "@/bridge/errors";

export function ConnectionOptionsDialog() {
  const profile = useDatabaseStore((s) =>
    s.profiles.find((p) => p.id === (s.profiles.find(p=>p.id===s.selectedProfileId)?.parentProfileId || s.selectedProfileId)),
  );
  if (!profile) return null;
  return <ConnectionOptionsForm key={profile.id} profile={profile} />;
}

/** Initialise from the target before paint; refetch never overwrites an edited field. */
function ConnectionOptionsForm({ profile }: { profile: ConnectionProfile }) {
  const { open, close, envTags, sshProfiles, fetchProfiles } = useDatabaseStore(
    useShallow((s) => ({
      open: s.showConnectionOptionsDialog,
      close: s.setShowConnectionOptionsDialog,
      envTags: s.envTags,
      sshProfiles: s.sshProfiles,
      fetchProfiles: s.fetchProfiles,
    })),
  );
  const active = useWorkbenchTabsStore((s) => s.activeModuleId === "database");
  const values = () => ({
    env: profile.env,
    readOnly: Boolean(profile.readOnly),
    trustedNetwork: Boolean(profile.trustedNetwork),
    sshProfileId: profile.sshProfileId || "",
  });
  const [form, setForm] = useState(values);
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setForm(values());
  }
  const [saving, setSaving] = useState(false);
  const operation = useRef(false);
  const [error, setError] = useState("");
  const [invalid, setInvalid] = useState<"env" | "ssh" | "">("");
  const prefix = useId();
  const fields = useRef<HTMLFieldSetElement>(null);
  const update = <K extends keyof typeof form>(
    key: K,
    value: (typeof form)[K],
  ) => {
    setForm((f) => ({ ...f, [key]: value }));
    setError("");
    setInvalid("");
  };
  const closeForm = () => {
    if (!operation.current) {
      close(false);
      setError("");
      setInvalid("");
    }
  };
  async function save() {
    if (operation.current) return;
    const invalidField = !envTags.some((t) => t.name === form.env)
      ? "env"
      : form.sshProfileId &&
          !sshProfiles.some((p) => p.id === form.sshProfileId)
        ? "ssh"
        : "";
    if (invalidField) {
      setInvalid(invalidField);
      fields.current
        ?.querySelector<HTMLElement>(`[data-field="${invalidField}"]`)
        ?.focus();
      return;
    }
    operation.current = true;
    setSaving(true);
    setError("");
    try {
      const updated = await updateProfileOptions(profile.id, {
        ...form,
        caCert: profile.caCert,
      });
      useDatabaseStore.setState((s) => ({
        profiles: s.profiles.map((p) => (p.id === updated.id ? updated : p)),
      }));
      const state = useDatabaseStore.getState();
      const selected = state.profiles.find(p => p.id === state.selectedProfileId);
      if ((selected?.parentProfileId || state.selectedProfileId) === profile.id) close(false);
      void fetchProfiles();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      operation.current = false;
      setSaving(false);
    }
  }
  return (
    <Dialog
      open={open && active}
      onOpenChange={(next) => {
        if (!next) closeForm();
      }}
    >
      <DialogContent
        className="max-h-[90dvh] overflow-y-auto sm:max-w-xl"
        showCloseButton={!saving}
        onEscapeKeyDown={(e) => {
          if (operation.current) e.preventDefault();
        }}
        onInteractOutside={(e) => {
          if (operation.current) e.preventDefault();
        }}
      >
        <DialogHeader>
          <DialogTitle>Connection Options · {profile.name}</DialogTitle>
          <DialogDescription>
            Choose the environment, write policy and tunnel for this connection.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <FieldSet ref={fields} disabled={saving}>
            <Field>
              <FieldLabel htmlFor={`${prefix}-env`}>
                Environment Policy Tag
              </FieldLabel>
              <NativeSelect
                id={`${prefix}-env`}
                data-field="env"
                value={form.env}
                onChange={(e) => update("env", e.target.value)}
                aria-invalid={invalid === "env"}
                aria-describedby={
                  invalid === "env" ? `${prefix}-env-error` : undefined
                }
              >
                <option value="">Choose environment…</option>
                {form.env && !envTags.some((t) => t.name === form.env) && (
                  <option value={form.env}>{form.env} (unavailable)</option>
                )}
                {envTags.map((t) => (
                  <option key={t.id} value={t.name}>
                    {t.name}
                  </option>
                ))}
              </NativeSelect>
              <FieldDescription>
                Controls confirmation for operations on this connection.
              </FieldDescription>
              {invalid === "env" && (
                <FieldError id={`${prefix}-env-error`}>
                  Choose an available environment.
                </FieldError>
              )}
            </Field>
            {envTags.find((t) => t.name === form.env)?.requiresConfirmation && (
              <FeedbackBanner tone="warning" title="Review required">
                This environment asks for confirmation before guarded
                operations.
              </FeedbackBanner>
            )}
            <Field orientation="horizontal">
              <FieldContent>
                <FieldLabel htmlFor={`${prefix}-readOnly`}>
                  Safe-Mode (Read-Only)
                </FieldLabel>
                <FieldDescription>
                  The database blocks writes. Changing this reconnects the
                  connection pool.
                </FieldDescription>
              </FieldContent>
              <Switch
                id={`${prefix}-readOnly`}
                disabled={saving}
                checked={form.readOnly}
                onCheckedChange={(v) => update("readOnly", v)}
              />
            </Field>
            {profile.sslMode === "disable" && (
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldLabel htmlFor={`${prefix}-trusted`}>
                    Network is already encrypted
                  </FieldLabel>
                  <FieldDescription>
                    Allow plaintext only when the path is protected by
                    Tailscale, WireGuard or a VPN.
                  </FieldDescription>
                </FieldContent>
                <Switch
                  id={`${prefix}-trusted`}
                  disabled={saving}
                  checked={form.trustedNetwork}
                  onCheckedChange={(v) => update("trustedNetwork", v)}
                />
              </Field>
            )}
            <Field>
              <FieldLabel htmlFor={`${prefix}-ssh`}>
                SSH Tunnel Forwarding
              </FieldLabel>
              <NativeSelect
                id={`${prefix}-ssh`}
                data-field="ssh"
                value={form.sshProfileId}
                onChange={(e) => update("sshProfileId", e.target.value)}
                aria-invalid={invalid === "ssh"}
                aria-describedby={
                  invalid === "ssh" ? `${prefix}-ssh-error` : undefined
                }
              >
                <option value="">Direct (No Tunnel)</option>
                {form.sshProfileId &&
                  !sshProfiles.some((p) => p.id === form.sshProfileId) && (
                    <option value={form.sshProfileId}>
                      Unavailable SSH profile
                    </option>
                  )}
                {sshProfiles.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </NativeSelect>
              <FieldDescription>
                {form.sshProfileId
                  ? `Tunnel target: ${profile.host}:${profile.port}`
                  : "Connect directly to the database."}
              </FieldDescription>
              {invalid === "ssh" && (
                <FieldError id={`${prefix}-ssh-error`}>
                  Choose an available SSH profile.
                </FieldError>
              )}
            </Field>
          </FieldSet>
          <div className="mt-3 min-h-5">
            {error && <InlineFeedback>{error}</InlineFeedback>}
          </div>
          <DialogFooter className="mt-3">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={saving}
              onClick={closeForm}
            >
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={saving}>
              {saving ? "Updating…" : "Save Options"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
