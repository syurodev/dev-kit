import { useState } from "react";
import { Database, X, AlertTriangle, Radio, ShieldCheck, Network } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { NativeSelect } from "@/components/ui/native-select";
import { viewIcon } from "@/modules/registry";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import { createProfile, testNewConnection } from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";

/** What each mode actually guarantees, in one line. */
const SSL_MODE_HINT: Record<string, string> = {
  disable: "No encryption. Loopback only, unless the network below is already encrypted.",
  require: "Encrypted, but the server's identity is not checked.",
  "verify-full": "Encrypted, and the certificate must match the host name.",
};

/** Mirrors isLoopbackHost in Go: the hosts sslmode=disable reaches unasked. */
function isLoopbackHost(host: string): boolean {
  const h = host.trim().toLowerCase();
  return h === "localhost" || h === "::1" || h.startsWith("127.");
}

/** The modes that read a CA bundle. */
function verifiesCertificate(sslMode: string): boolean {
  return sslMode === "verify-full" || sslMode === "verify-ca";
}

/**
 * One height for every field label, icon or not.
 *
 * The SSH label carries a glyph and the SSL one beside it does not, so the two
 * labels were different heights and the two selects under them sat six pixels
 * apart. A fixed row for the label means a label can gain an icon without
 * moving the field below it.
 */
const FIELD_LABEL = "flex h-4 items-center gap-1 font-medium text-text-primary";

export function NewConnectionDialog() {
  const {
    showNewConnectionDialog,
    setShowNewConnectionDialog,
    envTags,
    sshProfiles,
    fetchProfiles,
    setSelectedProfileId,
  } = useDatabaseStore(
    useShallow((s) => ({
      showNewConnectionDialog: s.showNewConnectionDialog,
      setShowNewConnectionDialog: s.setShowNewConnectionDialog,
      envTags: s.envTags,
      sshProfiles: s.sshProfiles,
      fetchProfiles: s.fetchProfiles,
      setSelectedProfileId: s.setSelectedProfileId,
    }))
  );

  const [engine, setEngine] = useState<"postgres" | "mysql" | "mariadb">("postgres");
  const [name, setName] = useState("");
  const [host, setHost] = useState("127.0.0.1");
  const [port, setPort] = useState(5432);
  const [database, setDatabase] = useState("");
  const [username, setUsername] = useState("postgres");
  const openTab = useWorkbenchTabsStore((state) => state.openTab);
  const [password, setPassword] = useState("");
  const [sslMode, setSslMode] = useState("disable");
  const [caCert, setCaCert] = useState("");
  const [trustedNetwork, setTrustedNetwork] = useState(false);
  const [env, setEnv] = useState("dev");
  const [sshProfileId, setSshProfileId] = useState("");

  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState("");
  const [error, setError] = useState("");

  const verifies = verifiesCertificate(sslMode);
  // A bundle typed under verify-full and then left behind by a switch to
  // require would be saved as a promise the connection does not keep; the
  // backend refuses that pairing, so drop it here rather than surface an error.
  const caForSubmit = verifies ? caCert : "";
  // Only asked when it decides something: plaintext to a host that is not
  // this machine. Anywhere else the answer is ignored, so it is not sent.
  const asksTrustedNetwork = sslMode === "disable" && !isLoopbackHost(host);
  const trustedForSubmit = asksTrustedNetwork && trustedNetwork;

  if (!showNewConnectionDialog) return null;

  function handleEngineChange(newEngine: "postgres" | "mysql" | "mariadb") {
    setEngine(newEngine);
    if (newEngine === "postgres") {
      setPort(5432);
      if (username === "root") setUsername("postgres");
    } else {
      setPort(3306);
      if (username === "postgres") setUsername("root");
    }
  }

  async function handleCreate() {
    // The database is optional, as in any SQL client: Postgres falls back to
    // the one named after the user, and MySQL connects with no default schema
    // and lists them all.
    if (!name.trim()) {
      setError("Please provide a connection name.");
      return;
    }

    setSaving(true);
    setError("");

    try {
      const created = await createProfile({
        name: name.trim(),
        engine,
        host: host.trim(),
        port: Number(port),
        database: database.trim(),
        username: username.trim(),
        password,
        sslMode,
        trustedNetwork: trustedForSubmit,
        env,
        sshProfileId,
        caCert: caForSubmit,
      });

      setShowNewConnectionDialog(false);
      await fetchProfiles();
      setSelectedProfileId(created.id);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  async function handleTest() {
    setTesting(true);
    setError("");
    setTestResult("");
    try {
      await testNewConnection({
        name: name.trim(),
        engine,
        host: host.trim(),
        port: Number(port),
        database: database.trim(),
        username: username.trim(),
        password,
        sslMode,
        trustedNetwork: trustedForSubmit,
        env,
        sshProfileId,
        caCert: caForSubmit,
      });
      setTestResult("Connected.");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setTesting(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4">
      <div className="bg-surface-base border border-border rounded-xl shadow-2xl w-full max-w-lg p-6 flex flex-col gap-4 animate-in fade-in zoom-in-95 duration-(--motion-base)">
        <div className="flex items-center justify-between border-b border-border pb-3">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-lg bg-action-primary/10 border border-action-primary/20 flex items-center justify-center text-action-primary">
              <Database className="w-4 h-4" />
            </div>
            <div>
              <h3 className="text-title-14 text-text-primary">New Database Connection</h3>
              <p className="text-caption-11 text-text-tertiary">
                Credentials are encrypted with your master vault key.
              </p>
            </div>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-text-tertiary hover:text-text-primary"
            onClick={() => setShowNewConnectionDialog(false)}
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        {/* Form Fields */}
        <div className="grid grid-cols-2 gap-3 text-body-12">
          {/* Connection Name */}
          <div className="col-span-2 space-y-1">
            <label className={FIELD_LABEL}>Connection Name</label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Production Analytics Replica"
              className="text-label-12 h-8"
              autoFocus
            />
          </div>

          {/* Engine Selector */}
          <div className="space-y-1">
            <label className={FIELD_LABEL}>Database Engine</label>
            <NativeSelect
              className="w-full"
              value={engine}
              onChange={(e) => handleEngineChange(e.target.value as any)}
              >
              <option value="postgres">PostgreSQL</option>
              <option value="mysql">MySQL</option>
              <option value="mariadb">MariaDB</option>
            </NativeSelect>
          </div>

          {/* Environment Tag */}
          <div className="space-y-1">
            <label className={FIELD_LABEL}>Environment Tag</label>
            <NativeSelect
              className="w-full"
              value={env}
              onChange={(e) => setEnv(e.target.value)}
              >
              {envTags.map((tag) => (
                <option key={tag.id} value={tag.name}>
                  {tag.name} {tag.name === "prod" ? "(Locked/Always-ask)" : ""}
                </option>
              ))}
            </NativeSelect>
          </div>

          {/* Host */}
          <div className="space-y-1">
            <label className={FIELD_LABEL}>Host / Server</label>
            <Input
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="127.0.0.1 or db.example.com"
              className="h-8 text-code-12"
            />
          </div>

          {/* Port */}
          <div className="space-y-1">
            <label className={FIELD_LABEL}>Port</label>
            <Input
              type="number"
              value={port}
              onChange={(e) => setPort(Number(e.target.value))}
              className="h-8 text-code-12"
            />
          </div>

          {/* Database */}
          <div className="space-y-1">
            <label className={FIELD_LABEL}>
              <span>Database Name</span>
              <span className="font-normal text-text-tertiary">optional</span>
            </label>
            <Input
              value={database}
              onChange={(e) => setDatabase(e.target.value)}
              placeholder={engine === "postgres" ? "defaults to the username" : "none: all schemas"}
              className="h-8 text-code-12"
            />
          </div>

          {/* Username */}
          <div className="space-y-1">
            <label className={FIELD_LABEL}>Username</label>
            <Input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="postgres"
              className="h-8 text-code-12"
            />
          </div>

          {/* Password */}
          <div className="col-span-2 space-y-1">
            <label className={FIELD_LABEL}>Password</label>
            <Input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••••••"
              className="h-8 text-code-12"
            />
          </div>

          {/* SSL Mode */}
          <div className="space-y-1">
            <label className={FIELD_LABEL}>SSL Mode</label>
            <NativeSelect
              className="w-full"
              value={sslMode}
              onChange={(e) => setSslMode(e.target.value)}
              >
              <option value="disable">disable (loopback or encrypted network)</option>
              <option value="require">require</option>
              <option value="verify-full">verify-full (recommended)</option>
            </NativeSelect>
            {/* Each mode makes a different promise, and the difference between
                the middle one and the last one is the whole point of the CA
                field below — so say it here rather than leaving it to be
                discovered by a failed handshake. */}
            <p className="text-caption-11 text-text-tertiary">{SSL_MODE_HINT[sslMode]}</p>
          </div>

          {/* Optional SSH Tunnel */}
          <div className="space-y-1">
            <label className={FIELD_LABEL}>
              <Radio className="size-3 text-status-info" />
              <span>SSH Tunnel Profile</span>
            </label>
            <NativeSelect
              className="w-full"
              value={sshProfileId}
              onChange={(e) => setSshProfileId(e.target.value)}
              >
              <option value="">Direct Connection (No Tunnel)</option>
              {sshProfiles.map((ssh) => (
                <option key={ssh.id} value={ssh.id}>
                  {ssh.name} ({ssh.username}@{ssh.host})
                </option>
              ))}
            </NativeSelect>
            {sshProfiles.length === 0 && (
              <p className="text-caption-11 text-text-tertiary">
                No SSH profiles yet.{" "}
                {/* The sentence used to name a place and leave you to find it.
                    writing.md asks a blank state to give the control, so it
                    does — and closing this dialog on the way is deliberate:
                    the profile has to exist before this field can offer it. */}
                <button
                  type="button"
                  className="text-brand-text underline underline-offset-2 hover:text-text-primary"
                  onClick={() => {
                    setShowNewConnectionDialog(false);
                    openTab({
                      moduleId: "ssh",
                      viewId: "ssh.connections",
                      title: "SSH",
                      icon: viewIcon("ssh", "ssh.connections"),
                    });
                  }}
                >
                  Add one in SSH
                </button>{" "}
                to reach a database behind a bastion.
              </p>
            )}
          </div>

          {/* A tailnet or VPN address is remote, but the path to it is already
              encrypted. DevKit cannot tell that network from the internet, so
              it asks rather than guesses. */}
          {asksTrustedNetwork && (
            <div className="col-span-2 flex items-center justify-between gap-3 rounded-lg border border-border bg-surface-raised p-2.5">
              <div className="space-y-0.5">
                <label
                  htmlFor="db-trusted-network"
                  className="flex items-center gap-1 font-medium text-text-primary"
                >
                  <Network className="size-3 text-status-info" />
                  <span>Network is already encrypted</span>
                </label>
                <p className="text-caption-11 text-text-tertiary">
                  Tailscale, WireGuard or a VPN. Without it, DevKit refuses plaintext to a remote
                  host and the password would cross the network unencrypted.
                </p>
              </div>
              <Switch
                id="db-trusted-network"
                checked={trustedNetwork}
                onCheckedChange={setTrustedNetwork}
              />
            </div>
          )}

          {/* The CA bundle only exists to make verify-full reachable, so it
              only appears under the mode that reads it. Mounted-but-hidden
              rather than unmounted would keep a stale certificate in state. */}
          {verifies && (
            <div className="col-span-2 space-y-1">
              <label className="font-medium text-text-primary flex items-center gap-1">
                <ShieldCheck className="w-3 h-3 text-status-info" />
                <span>CA Certificate</span>
                <span className="font-normal text-text-tertiary">optional</span>
              </label>
              <textarea
                value={caCert}
                onChange={(e) => setCaCert(e.target.value)}
                rows={4}
                spellCheck={false}
                placeholder={"-----BEGIN CERTIFICATE-----\n…\n-----END CERTIFICATE-----"}
                className="w-full rounded-lg border border-border bg-surface-raised px-2.5 py-2 text-code-11 text-text-primary outline-none focus:border-action-primary resize-y"
              />
              <p className="text-caption-11 text-text-tertiary">
                Leave empty to use this machine&rsquo;s trust store. Managed databases sign with
                their own authority — Amazon RDS and Aurora need the RDS CA bundle pasted here, or
                verify-full cannot succeed.
              </p>
            </div>
          )}
        </div>

        {error && (
          <div className="p-2.5 rounded bg-status-danger/10 border border-status-danger/30 text-body-12 text-status-danger flex items-center gap-2">
            <AlertTriangle className="w-4 h-4 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        {/* Dialog Actions */}
        <div className="flex items-center gap-2 pt-3 border-t border-border">
          {/* Dialling before saving is the point: otherwise the only way to
              learn a profile is wrong is to commit it first. */}
          <Button
            variant="secondary"
            size="sm"
            onClick={handleTest}
            disabled={testing || saving}
          >
            {testing ? "Testing…" : "Test connection"}
          </Button>
          {testResult && (
            <span className="text-body-12 text-status-success">{testResult}</span>
          )}
          <div className="flex-1" />
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowNewConnectionDialog(false)}
            disabled={saving}
          >
            Cancel
          </Button>
          <Button
            variant="default"
            size="sm"
            onClick={handleCreate}
            disabled={saving}
          >
            {saving ? "Saving…" : "Save Connection"}
          </Button>
        </div>
      </div>
    </div>
  );
}
