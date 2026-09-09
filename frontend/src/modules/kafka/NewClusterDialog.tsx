import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { errorMessage } from "@/bridge/errors";
import { isSaslProtocol, parseBootstrap, SASL_MECHANISMS, SECURITY_PROTOCOLS } from "./kafkaForm";
import { useKafkaStore } from "./kafkaStore";

const LABEL = "text-label-12 text-text-secondary";
const FIELD = "h-9 border-border bg-surface-well text-code-12";

/**
 * Figma `Kafka / New Cluster Dialog` (1154:925). The form genuinely branches:
 * isSaslProtocol() decides whether a mechanism, a username and a password
 * exist at all, so the SASL fields are not greyed out — they are absent.
 *
 * No "Test connection" here: KafkaTestConnection takes a saved profile's id,
 * so a form that has not been saved has nothing to test. The rail offers it
 * on every saved cluster instead.
 */
export function NewClusterDialog() {
  const open = useKafkaStore((s) => s.showNewCluster);
  const setOpen = useKafkaStore((s) => s.setShowNewCluster);
  const envTags = useKafkaStore((s) => s.envTags);
  const createProfile = useKafkaStore((s) => s.createProfile);

  const [name, setName] = useState("");
  const [bootstrap, setBootstrap] = useState("127.0.0.1:9092");
  const [protocol, setProtocol] = useState<string>("PLAINTEXT");
  const [mechanism, setMechanism] = useState<string>("PLAIN");
  const [username, setUsername] = useState("");
  const [secret, setSecret] = useState("");
  const [env, setEnv] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const sasl = isSaslProtocol(protocol);
  const envValue = env || envTags[0]?.name || "";
  const servers = parseBootstrap(bootstrap);
  const canSave = name.trim() !== "" && servers.length > 0 && envValue !== "" && (!sasl || username !== "");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSave) return;
    setBusy(true);
    setError("");
    try {
      await createProfile({
        name: name.trim(),
        bootstrapServers: servers,
        securityProtocol: protocol,
        saslMechanism: sasl ? mechanism : "",
        username: sasl ? username : "",
        secret,
        env: envValue,
      });
      setName("");
      setSecret("");
      setUsername("");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-w-140 px-6 pt-5 pb-5 sm:max-w-140">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle className="text-title-16">New Kafka cluster</DialogTitle>
            <DialogDescription className="sr-only">Save a connection profile for a Kafka cluster.</DialogDescription>
          </DialogHeader>

          <label className="flex flex-col gap-1.5">
            <span className={LABEL}>Name</span>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="local-kafka" autoFocus required className={FIELD} />
          </label>

          <label className="flex flex-col gap-1.5">
            <span className={LABEL}>Bootstrap servers</span>
            <Input value={bootstrap} onChange={(e) => setBootstrap(e.target.value)} placeholder="host:9092, host:9093" required className={FIELD} />
            <span className="text-body-12 text-text-tertiary">Comma-separated. DevKit splits and trims them into a list before saving.</span>
          </label>

          <div className="flex flex-col gap-1.5">
            <span className={LABEL}>Security protocol</span>
            <Tabs value={protocol} onValueChange={setProtocol} className="gap-0">
              <TabsList>
                {SECURITY_PROTOCOLS.map((p) => (
                  <TabsTrigger key={p} value={p}>{p}</TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          </div>

          {sasl && (
            <>
              <div className="flex flex-col gap-1.5">
                <span className={LABEL}>SASL mechanism</span>
                <Tabs value={mechanism} onValueChange={setMechanism} className="gap-0">
                  <TabsList>
                    {SASL_MECHANISMS.map((m) => (
                      <TabsTrigger key={m} value={m}>{m}</TabsTrigger>
                    ))}
                  </TabsList>
                </Tabs>
              </div>
              <label className="flex flex-col gap-1.5">
                <span className={LABEL}>Username</span>
                <Input value={username} onChange={(e) => setUsername(e.target.value)} required className={FIELD} />
              </label>
            </>
          )}

          <label className="flex flex-col gap-1.5">
            <span className={LABEL}>{sasl ? "SASL password" : "Secret"}</span>
            <Input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} placeholder={sasl ? "" : "optional — SSL client key"} className={FIELD} />
            <span className="text-body-12 text-text-tertiary">
              Encrypted into the vault envelope on save — the same contract SSH and Database secrets use. Never logged, never leaves this device in plaintext.
            </span>
          </label>

          <div className="flex flex-col gap-1.5">
            <span className={LABEL}>Environment</span>
            <Tabs value={envValue} onValueChange={setEnv} className="gap-0">
              <TabsList>
                {envTags.map((tag) => (
                  <TabsTrigger
                    key={tag.id}
                    value={tag.name}
                    // A tag that always asks is drawn in the colour of what it guards.
                    className={cn(tag.requiresConfirmation && "data-active:text-status-danger")}
                  >
                    {tag.name}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
            <span className="text-body-12 text-text-tertiary">
              Required — there is no default. The tag decides whether an agent has to ask before it touches this cluster, and prod always asks.
            </span>
          </div>

          {error && <p role="alert" className="text-body-12 text-status-danger">{error}</p>}

          <DialogFooter className="gap-2">
            <Button type="button" variant="outline" className="bg-surface-raised" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !canSave}>
              {busy ? "Creating…" : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
