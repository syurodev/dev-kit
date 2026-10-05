import { useEffect, useId, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { NativeSelect } from "@/components/ui/native-select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldLabel,
  FieldSet,
} from "@/components/ui/field";
import { InlineFeedback } from "@/components/ui/feedback";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import {
  createProfile,
  testNewConnection,
  type CreateProfileInput,
} from "@/bridge/database";
import { errorMessage, isCancelError } from "@/bridge/errors";
import {
  validateConnection,
  type ConnectionErrors,
  type ConnectionField,
} from "../connectionValidation";
import { ProbeStatus } from "./ProbeStatus";

const SSL_MODE_HINT: Record<string, string> = {
  disable:
    "No encryption. Loopback only, unless the network below is already encrypted.",
  require: "Encrypted, but the server's identity is not checked.",
  "verify-full": "Encrypted, and the certificate must match the host name.",
};
function verifiesCertificate(mode: string) {
  return mode === "verify-full" || mode === "verify-ca";
}
function isLoopbackHost(host: string) {
  const h = host.trim().toLowerCase();
  return h === "localhost" || h === "::1" || h.startsWith("127.");
}
const initial = (): CreateProfileInput => ({
  name: "",
  engine: "postgres",
  host: "127.0.0.1",
  port: 5432,
  database: "",
  username: "postgres",
  password: "",
  sslMode: "disable",
  env: "",
  sshProfileId: "",
  trustedNetwork: false,
  caCert: "",
});

export function NewConnectionDialog() {
  const { open, envTags, sshProfiles, fetchProfiles, close } = useDatabaseStore(
    useShallow((s) => ({
      open: s.showNewConnectionDialog,
      envTags: s.envTags,
      sshProfiles: s.sshProfiles,
      fetchProfiles: s.fetchProfiles,
      close: s.setShowNewConnectionDialog,
    })),
  );
  const active = useWorkbenchTabsStore((s) => s.activeModuleId === "database");
  // Credentials stay local to the form. Hiding the module preserves the draft;
  // explicit close or successful save clears it. Nothing is persisted in UI state.
  const [form, setForm] = useState(initial);
  const [errors, setErrors] = useState<ConnectionErrors>({});
  const [error, setError] = useState("");
  const [receipt, setReceipt] = useState("");
  const [pending, setPending] = useState<"test" | "save" | null>(null);
  const [startedAt, setStartedAt] = useState(0);
  const operation = useRef<"test" | "save" | null>(null);
  const probe = useRef<ReturnType<typeof testNewConnection> | null>(null);
  const generation = useRef(0);
  const fields = useRef<HTMLFieldSetElement>(null);
  const prefix = useId();
  useEffect(
    () => () => {
      generation.current++;
      probe.current?.cancel();
    },
    [],
  );
  const { sslMode, caCert } = form;
  const verifies = verifiesCertificate(sslMode);
  const caForSubmit = verifies ? caCert : "";
  const asksTrusted = sslMode === "disable" && !isLoopbackHost(form.host);
  const busy = pending !== null;
  const update = <K extends keyof CreateProfileInput>(
    key: K,
    value: CreateProfileInput[K],
  ) => {
    setForm((f) => ({ ...f, [key]: value }));
    setErrors({});
    setError("");
    setReceipt("");
  };
  const closeForm = () => {
    if (operation.current) return;
    setForm(initial());
    setErrors({});
    setError("");
    setReceipt("");
    close(false);
  };
  const cancelTest = () => {
    generation.current++;
    probe.current?.cancel();
    probe.current = null;
    operation.current = null;
    setPending(null);
    setReceipt("Connection test cancelled.");
  };
  async function submit(kind: "test" | "save") {
    if (operation.current) return;
    const input = {
      ...form,
      name: form.name.trim(),
      host: form.host.trim(),
      username: form.username.trim(),
      database: form.database.trim(),
      caCert: caForSubmit,
      trustedNetwork: asksTrusted && Boolean(form.trustedNetwork),
    };
    const invalid = validateConnection(
      input,
      envTags.map((t) => t.name),
      sshProfiles.map((p) => p.id),
    );
    setErrors(invalid);
    setError("");
    if (Object.keys(invalid).length) {
      fields.current
        ?.querySelector<HTMLElement>(
          `[data-field="${Object.keys(invalid)[0]}"]`,
        )
        ?.focus();
      return;
    }
    operation.current = kind;
    setPending(kind);
    setReceipt("");
    const token = ++generation.current;
    const start = Date.now();
    setStartedAt(start);
    const selectedAtStart = useDatabaseStore.getState().selectedProfileId;
    try {
      if (kind === "test") {
        const call = testNewConnection(input);
        probe.current = call;
        await call;
        if (generation.current === token)
          setReceipt(`Connected · ${Date.now() - start} ms`);
      } else {
        const created = await createProfile(input);
        useDatabaseStore.setState((s) => ({
          profiles: [...s.profiles.filter((p) => p.id !== created.id), created],
        }));
        if (generation.current !== token) return;
        setForm(initial());
        close(false);
        if (useDatabaseStore.getState().selectedProfileId === selectedAtStart)
          useDatabaseStore.getState().setSelectedProfileId(created.id);
        void fetchProfiles();
      }
    } catch (e) {
      if (generation.current === token && !isCancelError(e))
        setError(errorMessage(e));
    } finally {
      if (generation.current === token) {
        operation.current = null;
        probe.current = null;
        setPending(null);
      }
    }
  }
  const attrs = (key: ConnectionField) => ({
    id: `${prefix}-${key}`,
    "data-field": key,
    "aria-invalid": Boolean(errors[key]),
    "aria-describedby": errors[key] ? `${prefix}-${key}-error` : undefined,
  });
  const feedback = (key: ConnectionField) =>
    errors[key] && (
      <FieldError id={`${prefix}-${key}-error`}>{errors[key]}</FieldError>
    );
  return (
    <Dialog
      open={open && active}
      onOpenChange={(next) => {
        if (!next) closeForm();
      }}
    >
      <DialogContent
        className="max-h-[90dvh] overflow-y-auto sm:max-w-xl"
        showCloseButton={!busy}
        onEscapeKeyDown={(e) => {
          if (operation.current) e.preventDefault();
        }}
        onInteractOutside={(e) => {
          if (operation.current) e.preventDefault();
        }}
      >
        <DialogHeader>
          <DialogTitle>New Database Connection</DialogTitle>
          <DialogDescription>
            Save a connection in your encrypted vault. Test it before saving.
          </DialogDescription>
        </DialogHeader>
        <form
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            void submit("save");
          }}
        >
          <FieldSet
            ref={fields}
            disabled={busy}
            className="grid grid-cols-1 gap-3 sm:grid-cols-2"
          >
            <Field className="sm:col-span-2">
              <FieldLabel htmlFor={`${prefix}-name`}>
                Connection Name
              </FieldLabel>
              <Input
                {...attrs("name")}
                value={form.name}
                onChange={(e) => update("name", e.target.value)}
                placeholder="e.g. Analytics Replica"
                autoComplete="off"
                autoFocus
              />
              {feedback("name")}
            </Field>
            <Field>
              <FieldLabel htmlFor={`${prefix}-engine`}>
                Database Engine
              </FieldLabel>
              <NativeSelect
                id={`${prefix}-engine`}
                value={form.engine}
                onChange={(e) => {
                  const engine = e.target.value;
                  setForm((f) => ({
                    ...f,
                    engine,
                    port: engine === "postgres" ? 5432 : 3306,
                    username: ["postgres", "root"].includes(f.username)
                      ? engine === "postgres"
                        ? "postgres"
                        : "root"
                      : f.username,
                  }));
                  setReceipt("");
                  setError("");
                }}
              >
                <option value="postgres">PostgreSQL</option>
                <option value="mysql">MySQL</option>
                <option value="mariadb">MariaDB</option>
              </NativeSelect>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${prefix}-env`}>Environment Tag</FieldLabel>
              <NativeSelect
                {...attrs("env")}
                value={form.env}
                onChange={(e) => update("env", e.target.value)}
              >
                <option value="">Choose environment…</option>
                {envTags.map((t) => (
                  <option key={t.id} value={t.name}>
                    {t.name}
                  </option>
                ))}
              </NativeSelect>
              {feedback("env")}
            </Field>
            <Field>
              <FieldLabel htmlFor={`${prefix}-host`}>Host / Server</FieldLabel>
              <Input
                {...attrs("host")}
                value={form.host}
                onChange={(e) => update("host", e.target.value)}
                autoComplete="off"
              />
              {feedback("host")}
            </Field>
            <Field>
              <FieldLabel htmlFor={`${prefix}-port`}>Port</FieldLabel>
              <Input
                {...attrs("port")}
                type="number"
                min={1}
                max={65535}
                value={form.port || ""}
                onChange={(e) => update("port", Number(e.target.value))}
              />
              {feedback("port")}
            </Field>
            <Field>
              <FieldLabel htmlFor={`${prefix}-database`}>
                Database Name (optional)
              </FieldLabel>
              <Input
                id={`${prefix}-database`}
                value={form.database}
                onChange={(e) => update("database", e.target.value)}
                placeholder={
                  form.engine === "postgres"
                    ? "Defaults to username"
                    : "All schemas"
                }
              />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${prefix}-username`}>Username</FieldLabel>
              <Input
                {...attrs("username")}
                value={form.username}
                onChange={(e) => update("username", e.target.value)}
                autoComplete="off"
              />
              {feedback("username")}
            </Field>
            <Field className="sm:col-span-2">
              <FieldLabel htmlFor={`${prefix}-password`}>Password</FieldLabel>
              <Input
                id={`${prefix}-password`}
                type="password"
                value={form.password}
                onChange={(e) => update("password", e.target.value)}
                autoComplete="new-password"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${prefix}-ssl`}>SSL Mode</FieldLabel>
              <NativeSelect
                id={`${prefix}-ssl`}
                value={sslMode}
                onChange={(e) => update("sslMode", e.target.value)}
              >
                <option value="disable">disable</option>
                <option value="require">require</option>
                <option value="verify-full">verify-full</option>
              </NativeSelect>
              <FieldDescription>{SSL_MODE_HINT[sslMode]}</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${prefix}-sshProfileId`}>
                SSH Tunnel Profile
              </FieldLabel>
              <NativeSelect
                {...attrs("sshProfileId")}
                value={form.sshProfileId}
                onChange={(e) => update("sshProfileId", e.target.value)}
              >
                <option value="">Direct connection</option>
                {sshProfiles.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </NativeSelect>
              {feedback("sshProfileId")}
              <FieldDescription>
                {sshProfiles.length
                  ? "Optional encrypted tunnel through a saved SSH connection."
                  : "Create an SSH profile in the SSH module to use a bastion."}
              </FieldDescription>
            </Field>
            {asksTrusted && (
              <Field orientation="horizontal" className="sm:col-span-2">
                <FieldContent>
                  <FieldLabel htmlFor={`${prefix}-trusted`}>
                    Network is already encrypted
                  </FieldLabel>
                  <FieldDescription>
                    Tailscale, WireGuard or a VPN. Plaintext to a remote host is
                    refused without this.
                  </FieldDescription>
                </FieldContent>
                <Switch
                  id={`${prefix}-trusted`}
                  disabled={busy}
                  checked={Boolean(form.trustedNetwork)}
                  onCheckedChange={(v) => update("trustedNetwork", v)}
                />
              </Field>
            )}
            {verifies && (
              <Field className="sm:col-span-2">
                <FieldLabel htmlFor={`${prefix}-ca`}>
                  CA Certificate (optional)
                </FieldLabel>
                <Textarea
                  id={`${prefix}-ca`}
                  value={caCert}
                  onChange={(e) => update("caCert", e.target.value)}
                  rows={4}
                  spellCheck={false}
                  className="text-code-11"
                  placeholder={
                    "-----BEGIN CERTIFICATE-----\n…\n-----END CERTIFICATE-----"
                  }
                />
                <FieldDescription>
                  Leave empty to use this machine’s trust store.
                </FieldDescription>
              </Field>
            )}
          </FieldSet>
          <div className="mt-3 min-h-5" aria-live="polite">
            {pending === "test" ? (
              <ProbeStatus startedAt={startedAt} />
            ) : error ? (
              <InlineFeedback>{error}</InlineFeedback>
            ) : receipt ? (
              <InlineFeedback tone="info">{receipt}</InlineFeedback>
            ) : null}
          </div>
          <DialogFooter className="mt-3 flex-wrap">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              disabled={busy}
              onClick={() => void submit("test")}
            >
              Test connection
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={pending !== "test"}
              onClick={cancelTest}
            >
              Cancel test
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={closeForm}
            >
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={busy}>
              {pending === "save" ? "Saving…" : "Save Connection"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
