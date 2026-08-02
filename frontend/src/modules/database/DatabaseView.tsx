import { useEffect, useState } from "react";
import {
  createProfile,
  deleteProfile,
  listProfiles,
  runQuery,
  testConnection,
  type ConnectionProfile,
  type QueryResult,
} from "../../bridge/database";
import { errorMessage } from "../../bridge/errors";

const emptyForm = {
  name: "",
  host: "127.0.0.1",
  port: 5432,
  database: "",
  username: "",
  password: "",
  sslMode: "disable",
};

export function DatabaseView() {
  const [profiles, setProfiles] = useState<ConnectionProfile[]>([]);
  const [form, setForm] = useState({ ...emptyForm });
  const [selectedId, setSelectedId] = useState("");
  const [sql, setSql] = useState("select 1 as n, 'hello' as greeting");
  const [result, setResult] = useState<QueryResult | null>(null);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

  async function refresh() {
    try {
      const list = await listProfiles();
      setProfiles(list);
      if (!selectedId && list.length > 0) {
        setSelectedId(list[0].id);
      }
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function onCreate() {
    setError("");
    setStatus("");
    try {
      const created = await createProfile(form);
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
      await testConnection(selectedId);
      setStatus("Connection OK");
    } catch (e) {
      setStatus("");
      setError(errorMessage(e));
    }
  }

  async function onRun() {
    setError("");
    setStatus("");
    try {
      setResult(await runQuery(selectedId, sql));
    } catch (e) {
      setResult(null);
      setError(errorMessage(e));
    }
  }

  return (
    <section className="database-view">
      <h2>Database (PostgreSQL)</h2>

      <div className="db-create">
        <input
          aria-label="db-name"
          placeholder="Profile name"
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
        />
        <input
          aria-label="db-host"
          placeholder="Host"
          value={form.host}
          onChange={(e) => setForm({ ...form, host: e.target.value })}
        />
        <input
          aria-label="db-port"
          type="number"
          placeholder="Port"
          value={form.port}
          onChange={(e) => setForm({ ...form, port: Number(e.target.value) })}
        />
        <input
          aria-label="db-database"
          placeholder="Database"
          value={form.database}
          onChange={(e) => setForm({ ...form, database: e.target.value })}
        />
        <input
          aria-label="db-username"
          placeholder="Username"
          value={form.username}
          onChange={(e) => setForm({ ...form, username: e.target.value })}
        />
        <input
          aria-label="db-password"
          type="password"
          placeholder="Password"
          value={form.password}
          onChange={(e) => setForm({ ...form, password: e.target.value })}
        />
        <button type="button" onClick={onCreate}>
          Save connection
        </button>
      </div>

      <div className="db-profiles">
        <label>
          Connection
          <select
            aria-label="db-profile-select"
            value={selectedId}
            onChange={(e) => setSelectedId(e.target.value)}
          >
            <option value="">— select —</option>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} ({p.username}@{p.host}:{p.port}/{p.database})
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
            aria-label="db-delete"
            onClick={async () => {
              await deleteProfile(selectedId);
              setSelectedId("");
              await refresh();
            }}
          >
            Delete
          </button>
        ) : null}
      </div>

      <div className="db-query">
        <textarea
          aria-label="db-sql"
          value={sql}
          onChange={(e) => setSql(e.target.value)}
        />
        <button type="button" onClick={onRun} disabled={!selectedId}>
          Run query
        </button>
      </div>

      {status ? <p className="db-status">{status}</p> : null}
      {error ? <p role="alert">{error}</p> : null}

      {result ? (
        <div className="db-result">
          <p>
            {result.rowCount} row(s){result.truncated ? " (truncated)" : ""}
          </p>
          <table aria-label="db-result-table">
            <thead>
              <tr>
                {result.columns.map((c) => (
                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {result.rows.map((row, i) => (
                <tr key={i}>
                  {row.map((cell, j) => (
                    <td key={j}>{cell}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  );
}
