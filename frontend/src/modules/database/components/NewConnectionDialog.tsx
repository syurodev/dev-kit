import { useState } from "react";
import { Database, X, AlertTriangle, Radio } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import { createProfile, testNewConnection } from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";

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
  const [password, setPassword] = useState("");
  const [sslMode, setSslMode] = useState("disable");
  const [env, setEnv] = useState("dev");
  const [sshProfileId, setSshProfileId] = useState("");

  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState("");
  const [error, setError] = useState("");

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
    if (!name.trim() || !database.trim()) {
      setError("Please provide a connection name and database name.");
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
        env,
        sshProfileId,
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
        env,
        sshProfileId,
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
            <label className="font-medium text-text-primary">Connection Name</label>
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
            <label className="font-medium text-text-primary">Database Engine</label>
            <select
              value={engine}
              onChange={(e) => handleEngineChange(e.target.value as any)}
              className="w-full h-8 px-2.5 rounded-lg border border-border bg-surface-raised text-body-13 text-text-primary outline-none focus:border-action-primary"
            >
              <option value="postgres">PostgreSQL</option>
              <option value="mysql">MySQL</option>
              <option value="mariadb">MariaDB</option>
            </select>
          </div>

          {/* Environment Tag */}
          <div className="space-y-1">
            <label className="font-medium text-text-primary">Environment Tag</label>
            <select
              value={env}
              onChange={(e) => setEnv(e.target.value)}
              className="w-full h-8 px-2.5 rounded-lg border border-border bg-surface-raised text-body-13 text-text-primary outline-none focus:border-action-primary"
            >
              {envTags.map((tag) => (
                <option key={tag.id} value={tag.name}>
                  {tag.name} {tag.name === "prod" ? "(Locked/Always-ask)" : ""}
                </option>
              ))}
            </select>
          </div>

          {/* Host */}
          <div className="space-y-1">
            <label className="font-medium text-text-primary">Host / Server</label>
            <Input
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="127.0.0.1 or db.example.com"
              className="h-8 text-code-12"
            />
          </div>

          {/* Port */}
          <div className="space-y-1">
            <label className="font-medium text-text-primary">Port</label>
            <Input
              type="number"
              value={port}
              onChange={(e) => setPort(Number(e.target.value))}
              className="h-8 text-code-12"
            />
          </div>

          {/* Database */}
          <div className="space-y-1">
            <label className="font-medium text-text-primary">Database Name</label>
            <Input
              value={database}
              onChange={(e) => setDatabase(e.target.value)}
              placeholder="mydb"
              className="h-8 text-code-12"
            />
          </div>

          {/* Username */}
          <div className="space-y-1">
            <label className="font-medium text-text-primary">Username</label>
            <Input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="postgres"
              className="h-8 text-code-12"
            />
          </div>

          {/* Password */}
          <div className="col-span-2 space-y-1">
            <label className="font-medium text-text-primary">Password</label>
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
            <label className="font-medium text-text-primary">SSL Mode</label>
            <select
              value={sslMode}
              onChange={(e) => setSslMode(e.target.value)}
              className="w-full h-8 px-2.5 rounded-lg border border-border bg-surface-raised text-body-13 text-text-primary outline-none focus:border-action-primary"
            >
              <option value="disable">disable (loopback dev only)</option>
              <option value="require">require</option>
              <option value="verify-full">verify-full (recommended)</option>
            </select>
          </div>

          {/* Optional SSH Tunnel */}
          <div className="space-y-1">
            <label className="font-medium text-text-primary flex items-center gap-1">
              <Radio className="w-3 h-3 text-status-info" />
              <span>SSH Tunnel Profile</span>
            </label>
            <select
              value={sshProfileId}
              onChange={(e) => setSshProfileId(e.target.value)}
              className="w-full h-8 px-2.5 rounded-lg border border-border bg-surface-raised text-body-13 text-text-primary outline-none focus:border-action-primary"
            >
              <option value="">Direct Connection (No Tunnel)</option>
              {sshProfiles.map((ssh) => (
                <option key={ssh.id} value={ssh.id}>
                  {ssh.name} ({ssh.username}@{ssh.host})
                </option>
              ))}
            </select>
          </div>
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
