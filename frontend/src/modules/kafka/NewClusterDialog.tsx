import { useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldLabel, FieldSet } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { InlineFeedback, PendingStatus } from "@/components/ui/feedback";
import { errorMessage } from "@/bridge/errors";
import { isSaslProtocol, parseBootstrap, SASL_MECHANISMS, SECURITY_PROTOCOLS, validateCluster } from "./kafkaForm";
import { useKafkaStore } from "./kafkaStore";

/** Owned by the module rail: closing the explorer does not lose this draft. */
export function NewClusterDialog({ visible = true }: { visible?: boolean }) {
  const open = useKafkaStore(s => s.showNewCluster);
  const setOpen = useKafkaStore(s => s.setShowNewCluster);
  const envTags = useKafkaStore(s => s.envTags);
  const createProfile = useKafkaStore(s => s.createProfile);
  const [name, setName] = useState("");
  const [bootstrap, setBootstrap] = useState("127.0.0.1:9092");
  const [protocol, setProtocol] = useState<string>("PLAINTEXT");
  const [mechanism, setMechanism] = useState<string>("PLAIN");
  const [username, setUsername] = useState("");
  const [secret, setSecret] = useState("");
  const [env, setEnv] = useState("");
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const formRef = useRef<HTMLFormElement>(null);
  const [error, setError] = useState("");
  const [errors, setErrors] = useState<ReturnType<typeof validateCluster>>({});
  const sasl = isSaslProtocol(protocol);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (submitting.current) return;
    const validation = validateCluster({ name, bootstrap, protocol, username, env });
    setErrors(validation);
    if (Object.keys(validation).length) {
      formRef.current?.querySelector<HTMLElement>(`#kafka-new-${Object.keys(validation)[0]}`)?.focus();
      return;
    }
    submitting.current = true; setBusy(true); setError("");
    try {
      await createProfile({ name: name.trim(), bootstrapServers: parseBootstrap(bootstrap), securityProtocol: protocol,
        saslMechanism: sasl ? mechanism : "", username: sasl ? username.trim() : "", secret, env });
      setName(""); setSecret(""); setUsername(""); setErrors({});
    } catch (err) { setError(errorMessage(err)); }
    finally { submitting.current = false; setBusy(false); }
  }
  const fieldError = (key: keyof typeof errors) => errors[key] && <FieldError id={`kafka-new-${key}-error`}>{errors[key]}</FieldError>;
  const validity = (key: keyof typeof errors) => ({ "aria-invalid": !!errors[key], "aria-describedby": errors[key] ? `kafka-new-${key}-error` : undefined });
  return <Dialog open={open && visible} onOpenChange={value => { if (!submitting.current) setOpen(value); }}>
    <DialogContent showCloseButton={!busy} className="max-h-[90vh] overflow-y-auto sm:max-w-140">
      <form ref={formRef} onSubmit={submit} noValidate className="flex flex-col gap-4">
        <DialogHeader><DialogTitle>New Kafka cluster</DialogTitle><DialogDescription>Save a cluster connection in your vault.</DialogDescription></DialogHeader>
        <FieldSet disabled={busy}>
          <Field data-invalid={!!errors.name}><FieldLabel htmlFor="kafka-new-name">Name</FieldLabel><Input id="kafka-new-name" value={name} onChange={e => setName(e.target.value)} placeholder="local-kafka" autoFocus {...validity("name")} />{fieldError("name")}</Field>
          <Field data-invalid={!!errors.bootstrap}><FieldLabel htmlFor="kafka-new-bootstrap">Bootstrap servers</FieldLabel><Input id="kafka-new-bootstrap" value={bootstrap} onChange={e => setBootstrap(e.target.value)} placeholder="host:9092, host:9093" {...validity("bootstrap")} /><FieldDescription>Comma-separated host:port addresses.</FieldDescription>{fieldError("bootstrap")}</Field>
          <Field><FieldLabel htmlFor="kafka-new-protocol">Security protocol</FieldLabel><NativeSelect id="kafka-new-protocol" value={protocol} onChange={e => setProtocol(e.target.value)}>{SECURITY_PROTOCOLS.map(p => <option key={p}>{p}</option>)}</NativeSelect></Field>
          {sasl && <>
            <Field><FieldLabel htmlFor="kafka-new-mechanism">SASL mechanism</FieldLabel><NativeSelect id="kafka-new-mechanism" value={mechanism} onChange={e => setMechanism(e.target.value)}>{SASL_MECHANISMS.map(m => <option key={m}>{m}</option>)}</NativeSelect></Field>
            <Field data-invalid={!!errors.username}><FieldLabel htmlFor="kafka-new-username">Username</FieldLabel><Input id="kafka-new-username" value={username} onChange={e => setUsername(e.target.value)} {...validity("username")} />{fieldError("username")}</Field>
          </>}
          <Field><FieldLabel htmlFor="kafka-new-secret">{sasl ? "SASL password" : "Secret"}</FieldLabel><Input id="kafka-new-secret" type="password" value={secret} onChange={e => setSecret(e.target.value)} autoComplete="new-password" placeholder={sasl ? "" : "optional — SSL client key"} /><FieldDescription>Encrypted in your vault when saved.</FieldDescription></Field>
          <Field data-invalid={!!errors.env}><FieldLabel htmlFor="kafka-new-env">Environment</FieldLabel><NativeSelect id="kafka-new-env" value={env} onChange={e => setEnv(e.target.value)} {...validity("env")}><option value="">Select environment</option>{envTags.map(tag => <option key={tag.id} value={tag.name}>{tag.name}</option>)}</NativeSelect><FieldDescription>Required. The environment controls confirmation for agent actions.</FieldDescription>{fieldError("env")}</Field>
        </FieldSet>
        {error && <InlineFeedback>{error}</InlineFeedback>}
        {busy && <PendingStatus>Creating cluster…</PendingStatus>}
        <DialogFooter><Button type="button" variant="outline" disabled={busy} onClick={() => setOpen(false)}>Cancel</Button><Button type="submit" disabled={busy}>{busy ? "Creating…" : "Create"}</Button></DialogFooter>
      </form>
    </DialogContent>
  </Dialog>;
}
