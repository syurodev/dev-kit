import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createSSHProfile,
  deleteSSHProfile,
  listSSHProfiles,
  listSSHForwards,
  observeSSHHostKey,
  runSSHCommand,
  startSSHForward,
  stopSSHForward,
  testSSHConnection,
  trustSSHHostKey,
  type SSHForwardDirection,
} from "../../bridge/ssh";
import { listEnvTags } from "../../bridge/environments";
import { errorMessage } from "../../bridge/errors";

const emptyForm = {
  name: "",
  host: "127.0.0.1",
  port: 22,
  username: "",
  authMethod: "password",
  secret: "",
  env: "dev",
};

export function SSHView() {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({ ...emptyForm });
  const [selectedId, setSelectedId] = useState("");
  const [command, setCommand] = useState("echo hello from $(hostname)");
  const [passphrase, setPassphrase] = useState("");
  const [observedHostKey, setObservedHostKey] = useState("");
  const [output, setOutput] = useState("");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [forwardDirection, setForwardDirection] = useState<SSHForwardDirection>("local");
  const [forwardListenHost, setForwardListenHost] = useState("127.0.0.1");
  const [forwardListenPort, setForwardListenPort] = useState(0);
  const [forwardTargetHost, setForwardTargetHost] = useState("127.0.0.1");
  const [forwardTargetPort, setForwardTargetPort] = useState(5432);

  const { data: profiles = [] } = useQuery({
    queryKey: ["ssh", "profiles"],
    queryFn: () => listSSHProfiles(),
  });

  const { data: forwards = [] } = useQuery({
    queryKey: ["ssh", "forwards"],
    queryFn: () => listSSHForwards(),
  });

  const { data: envTags = [] } = useQuery({
    queryKey: ["environments", "tags"],
    queryFn: () => listEnvTags(),
  });

  useEffect(() => {
    if (!selectedId && profiles.length > 0) {
      setSelectedId(profiles[0].id);
    }
  }, [selectedId, profiles]);

  async function refresh() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["ssh", "profiles"] }),
      queryClient.invalidateQueries({ queryKey: ["ssh", "forwards"] }),
      queryClient.invalidateQueries({ queryKey: ["environments", "tags"] }),
    ]);
  }

  async function onCreate() {
    setError("");
    setStatus("");
    try {
      const created = await createSSHProfile(form);
      setForm({ ...emptyForm });
      setSelectedId(created.id);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  async function onTest() {
    setError("");
    setStatus("Testing…");
    try {
      await testSSHConnection(selectedId, passphrase);
      setStatus("Connection OK");
    } catch (e) {
      setStatus("");
      setError(errorMessage(e));
    } finally {
      setPassphrase("");
    }
  }

  async function onRun() {
    setError("");
    setStatus("");
    try {
      setOutput(await runSSHCommand(selectedId, command, passphrase));
    } catch (e) {
      setOutput("");
      setError(errorMessage(e));
    } finally {
      setPassphrase("");
    }
  }

  async function onObserveHostKey() {
    setError("");
    setStatus("Observing host key…");
    try {
      setObservedHostKey(await observeSSHHostKey(selectedId));
      setStatus("Review the fingerprint, then explicitly trust it.");
    } catch (e) {
      setStatus("");
      setError(errorMessage(e));
    }
  }

  async function onTrustHostKey() {
    setError("");
    try {
      await trustSSHHostKey(selectedId, observedHostKey);
      setObservedHostKey("");
      setStatus("Host key trusted.");
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  async function onStartForward() {
    if (!selectedId) return;
    setError("");
    setStatus("Starting SSH forward…");
    try {
      const started = await startSSHForward(
        selectedId,
        {
          direction: forwardDirection,
          listenHost: forwardListenHost,
          listenPort: forwardListenPort,
          targetHost: forwardTargetHost,
          targetPort: forwardTargetPort,
        },
        passphrase,
      );
      void queryClient.invalidateQueries({ queryKey: ["ssh", "forwards"] });
      setStatus(`Forward started on ${started.listenHost}:${started.listenPort}.`);
    } catch (e) {
      setStatus("");
      setError(errorMessage(e));
    } finally {
      setPassphrase("");
    }
  }

  async function onStopForward(id: string) {
    setError("");
    try {
      await stopSSHForward(id);
      void queryClient.invalidateQueries({ queryKey: ["ssh", "forwards"] });
      setStatus("Forward stopped.");
    } catch (e) {
      setError(errorMessage(e));
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
        <select
          aria-label="ssh-env"
          value={form.env}
          onChange={(e) => setForm({ ...form, env: e.target.value })}
        >
          {envTags.map((tag) => (
            <option key={tag.id} value={tag.name}>
              {tag.name}
            </option>
          ))}
        </select>
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
        <button type="button" onClick={onObserveHostKey} disabled={!selectedId}>
          Observe host key
        </button>
        <button
          type="button"
          onClick={onTrustHostKey}
          disabled={!selectedId || !observedHostKey}
        >
          Trust observed host key
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

      {selectedId ? (
        <div className="ssh-host-key">
          <p>
            Trusted host key: {profiles.find((profile) => profile.id === selectedId)?.hostKeyFingerprint || "not trusted"}
          </p>
          {observedHostKey ? <code aria-label="ssh-observed-host-key">{observedHostKey}</code> : null}
        </div>
      ) : null}

      <div className="ssh-forward">
        <h3>SSH tunnel / forwarding</h3>
        <p>
          Forward listeners are loopback-only and live until stopped or the app exits.
        </p>
        <select
          aria-label="ssh-forward-direction"
          value={forwardDirection}
          onChange={(e) => setForwardDirection(e.target.value as SSHForwardDirection)}
        >
          <option value="local">Local forward (-L)</option>
          <option value="remote">Remote forward (-R)</option>
        </select>
        <input
          aria-label="ssh-forward-listen-host"
          placeholder="Listen host (loopback)"
          value={forwardListenHost}
          onChange={(e) => setForwardListenHost(e.target.value)}
        />
        <input
          aria-label="ssh-forward-listen-port"
          type="number"
          min={0}
          max={65535}
          placeholder="Listen port (0 = automatic)"
          value={forwardListenPort}
          onChange={(e) => setForwardListenPort(Number(e.target.value))}
        />
        <input
          aria-label="ssh-forward-target-host"
          placeholder="Target host"
          value={forwardTargetHost}
          onChange={(e) => setForwardTargetHost(e.target.value)}
        />
        <input
          aria-label="ssh-forward-target-port"
          type="number"
          min={1}
          max={65535}
          placeholder="Target port"
          value={forwardTargetPort}
          onChange={(e) => setForwardTargetPort(Number(e.target.value))}
        />
        <button type="button" onClick={onStartForward} disabled={!selectedId}>
          Start forwarding
        </button>
        {forwards.length > 0 ? (
          <ul aria-label="ssh-active-forwards">
            {forwards.map((forward) => (
              <li key={forward.id}>
                <code>
                  {forward.direction} {forward.listenHost}:{forward.listenPort} → {forward.targetHost}:{forward.targetPort}
                </code>
                <button type="button" onClick={() => void onStopForward(forward.id)}>
                  Stop
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </div>

      <label>
        Private-key passphrase (used once)
        <input
          aria-label="ssh-passphrase"
          type="password"
          autoComplete="off"
          value={passphrase}
          onChange={(e) => setPassphrase(e.target.value)}
        />
      </label>

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
