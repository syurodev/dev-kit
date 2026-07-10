import { useEffect, useState } from "react";
import {
  createSSHProfile,
  deleteSSHProfile,
  listSSHProfiles,
  runSSHCommand,
  testSSHConnection,
  type SSHProfile,
} from "../../bridge/ssh";

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

const emptyForm = {
  name: "",
  host: "127.0.0.1",
  port: 22,
  username: "",
  authMethod: "password",
  secret: "",
};

export function SSHView() {
  const [profiles, setProfiles] = useState<SSHProfile[]>([]);
  const [form, setForm] = useState({ ...emptyForm });
  const [selectedId, setSelectedId] = useState("");
  const [command, setCommand] = useState("echo hello from $(hostname)");
  const [output, setOutput] = useState("");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

  async function refresh() {
    try {
      const list = await listSSHProfiles();
      setProfiles(list);
      if (!selectedId && list.length > 0) {
        setSelectedId(list[0].id);
      }
    } catch (e) {
      setError(message(e));
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function onCreate() {
    setError("");
    setStatus("");
    try {
      const created = await createSSHProfile(form);
      setForm({ ...emptyForm });
      setSelectedId(created.id);
      await refresh();
    } catch (e) {
      setError(message(e));
    }
  }

  async function onTest() {
    setError("");
    setStatus("Testing…");
    try {
      await testSSHConnection(selectedId);
      setStatus("Connection OK");
    } catch (e) {
      setStatus("");
      setError(message(e));
    }
  }

  async function onRun() {
    setError("");
    setStatus("");
    try {
      setOutput(await runSSHCommand(selectedId, command));
    } catch (e) {
      setOutput("");
      setError(message(e));
    }
  }

  return (
    <section className="ssh-view">
      <h2>SSH</h2>

      <div className="ssh-create">
        <input
          aria-label="ssh-name"
          placeholder="Profile name"
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
        />
        <input
          aria-label="ssh-host"
          placeholder="Host"
          value={form.host}
          onChange={(e) => setForm({ ...form, host: e.target.value })}
        />
        <input
          aria-label="ssh-port"
          type="number"
          placeholder="Port"
          value={form.port}
          onChange={(e) => setForm({ ...form, port: Number(e.target.value) })}
        />
        <input
          aria-label="ssh-username"
          placeholder="Username"
          value={form.username}
          onChange={(e) => setForm({ ...form, username: e.target.value })}
        />
        <select
          aria-label="ssh-auth"
          value={form.authMethod}
          onChange={(e) => setForm({ ...form, authMethod: e.target.value })}
        >
          <option value="password">Password</option>
          <option value="key">Private key</option>
        </select>
        <textarea
          aria-label="ssh-secret"
          placeholder={
            form.authMethod === "key" ? "Paste private key (PEM)" : "Password"
          }
          value={form.secret}
          onChange={(e) => setForm({ ...form, secret: e.target.value })}
        />
        <button type="button" onClick={onCreate}>
          Save connection
        </button>
      </div>

      <div className="ssh-profiles">
        <label>
          Connection
          <select
            aria-label="ssh-profile-select"
            value={selectedId}
            onChange={(e) => setSelectedId(e.target.value)}
          >
            <option value="">— select —</option>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} ({p.username}@{p.host}:{p.port})
              </option>
            ))}
          </select>
        </label>
        <button type="button" onClick={onTest} disabled={!selectedId}>
          Test connection
        </button>
        {selectedId ? (
          <button
            type="button"
            aria-label="ssh-delete"
            onClick={async () => {
              await deleteSSHProfile(selectedId);
              setSelectedId("");
              await refresh();
            }}
          >
            Delete
          </button>
        ) : null}
      </div>

      <div className="ssh-command">
        <input
          aria-label="ssh-cmd"
          value={command}
          onChange={(e) => setCommand(e.target.value)}
        />
        <button type="button" onClick={onRun} disabled={!selectedId}>
          Run command
        </button>
      </div>

      {status ? <p className="db-status">{status}</p> : null}
      {error ? <p role="alert">{error}</p> : null}
      {output ? <pre aria-label="ssh-output">{output}</pre> : null}
    </section>
  );
}
