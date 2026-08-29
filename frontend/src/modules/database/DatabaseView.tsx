import { useEffect, useState } from "react";
import {
  createProfile,
  deleteProfile,
  listProfiles,
  runQuery,
  testConnection,
  updateProfileOptions,
  type ConnectionProfile,
  type QueryResult,
} from "../../bridge/database";
import { listEnvTags, type EnvTag } from "../../bridge/environments";
import { errorMessage } from "../../bridge/errors";
import { exportQueryResult } from "./export";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";

const emptyForm = {
  name: "",
  host: "127.0.0.1",
  port: 5432,
  database: "",
  username: "",
  password: "",
  sslMode: "disable",
  env: "dev",
};

export function DatabaseView() {
  const [profiles, setProfiles] = useState<ConnectionProfile[]>([]);
  const [envTags, setEnvTags] = useState<EnvTag[]>([]);
  const [form, setForm] = useState({ ...emptyForm });
  const [selectedId, setSelectedId] = useState("");
  const [sql, setSql] = useState("select 1 as n, 'hello' as greeting");
  const [result, setResult] = useState<QueryResult | null>(null);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

  // Connection options edit state for selected profile
  const [optionsEnv, setOptionsEnv] = useState("dev");
  const [optionsReadOnly, setOptionsReadOnly] = useState(false);
  const [showOptions, setShowOptions] = useState(false);

  const selectedProfile = profiles.find((p) => p.id === selectedId);

  useEffect(() => {
    if (selectedProfile) {
      setOptionsEnv(selectedProfile.env || "dev");
      setOptionsReadOnly(Boolean(selectedProfile.readOnly));
    }
  }, [selectedProfile]);

  async function refresh() {
    try {
      const [list, tags] = await Promise.all([listProfiles(), listEnvTags()]);
      setProfiles(list);
      setEnvTags(tags);
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

  async function onSaveOptions() {
    if (!selectedId) return;
    setError("");
    setStatus("");
    try {
      await updateProfileOptions(selectedId, {
        env: optionsEnv,
        readOnly: optionsReadOnly,
      });
      setStatus("Connection options updated");
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

  async function onExport(format: "csv" | "json") {
    if (!result) return;
    setError("");
    setStatus("");
    try {
      const path = await exportQueryResult(result, format);
      if (path) {
        setStatus(`Exported to ${path}`);
      }
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  const isReadOnlyError =
    error.toLowerCase().includes("read-only") || error.includes("25006");

  return (
    <section className="database-view p-6 flex flex-col gap-6 max-w-5xl mx-auto">
      <div className="flex items-center justify-between border-b border-border pb-4">
        <div>
          <h2 className="text-xl font-semibold text-text-primary">Database (PostgreSQL)</h2>
          <p className="text-xs text-text-tertiary mt-0.5">
            Manage PostgreSQL profiles, safe-mode execution, and export query results.
          </p>
        </div>
      </div>

      {/* Profile Creation Form */}
      <div className="db-create p-4 rounded-xl bg-surface-raised border border-border flex flex-col gap-3">
        <h3 className="text-sm font-medium text-text-primary">Create Profile</h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <input
            className="h-8 px-3 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-status-info"
            aria-label="db-name"
            placeholder="Profile name"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
          <input
            className="h-8 px-3 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-status-info"
            aria-label="db-host"
            placeholder="Host"
            value={form.host}
            onChange={(e) => setForm({ ...form, host: e.target.value })}
          />
          <input
            className="h-8 px-3 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-status-info"
            aria-label="db-port"
            type="number"
            placeholder="Port"
            value={form.port}
            onChange={(e) => setForm({ ...form, port: Number(e.target.value) })}
          />
          <input
            className="h-8 px-3 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-status-info"
            aria-label="db-database"
            placeholder="Database"
            value={form.database}
            onChange={(e) => setForm({ ...form, database: e.target.value })}
          />
          <input
            className="h-8 px-3 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-status-info"
            aria-label="db-username"
            placeholder="Username"
            value={form.username}
            onChange={(e) => setForm({ ...form, username: e.target.value })}
          />
          <input
            className="h-8 px-3 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-status-info"
            aria-label="db-password"
            type="password"
            placeholder="Password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
          />
          <select
            className="h-8 px-2 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-status-info"
            aria-label="db-env"
            value={form.env}
            onChange={(e) => setForm({ ...form, env: e.target.value })}
          >
            {envTags.map((tag) => (
              <option key={tag.id} value={tag.name}>
                {tag.name}
              </option>
            ))}
          </select>
          <Button type="button" size="sm" onClick={onCreate}>
            Save connection
          </Button>
        </div>
      </div>

      {/* Profiles Selection & Actions */}
      <div className="db-profiles p-4 rounded-xl bg-surface-raised border border-border flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <label className="flex items-center gap-2 text-xs font-medium text-text-secondary">
            Connection
            <select
              className="h-8 px-2 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-status-info min-w-[280px]"
              aria-label="db-profile-select"
              value={selectedId}
              onChange={(e) => setSelectedId(e.target.value)}
            >
              <option value="">— select profile —</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.username}@{p.host}:{p.port}/{p.database}) [{p.env}]
                  {p.readOnly ? " [Safe-mode]" : ""}
                </option>
              ))}
            </select>
          </label>

          <div className="flex items-center gap-2">
            {selectedProfile ? (
              <>
                <Badge variant="outline" className="text-[11px]">
                  Env: {selectedProfile.env}
                </Badge>
                {selectedProfile.readOnly ? (
                  <Badge variant="destructive" className="text-[11px]">
                    Safe-mode (Read-Only)
                  </Badge>
                ) : (
                  <Badge variant="secondary" className="text-[11px]">
                    Read/Write
                  </Badge>
                )}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setShowOptions(!showOptions)}
                >
                  {showOptions ? "Hide Options" : "Connection Options"}
                </Button>
              </>
            ) : null}

            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={onTest}
              disabled={!selectedId}
            >
              Test connection
            </Button>
            {selectedId ? (
              <Button
                type="button"
                variant="destructive"
                size="sm"
                aria-label="db-delete"
                onClick={async () => {
                  await deleteProfile(selectedId);
                  setSelectedId("");
                  await refresh();
                }}
              >
                Delete
              </Button>
            ) : null}
          </div>
        </div>

        {/* Connection Options Panel */}
        {showOptions && selectedProfile ? (
          <div className="mt-2 p-3 rounded-lg border border-(--glass-border) bg-(--glass-panel-bg) flex flex-col gap-3">
            <div className="flex items-center justify-between border-b border-border/50 pb-2">
              <span className="text-xs font-semibold text-text-primary">
                Connection Options · {selectedProfile.name}
              </span>
              <span className="text-[11px] text-text-tertiary">
                Metadata-only write path (does not re-encrypt password)
              </span>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 items-center">
              <label className="flex items-center justify-between gap-2 text-xs text-text-secondary">
                <span>Environment Tag (policy gateway)</span>
                <select
                  className="h-8 px-2 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-status-info min-w-[140px]"
                  aria-label="db-options-env"
                  value={optionsEnv}
                  onChange={(e) => setOptionsEnv(e.target.value)}
                >
                  {envTags.map((tag) => (
                    <option key={tag.id} value={tag.name}>
                      {tag.name}
                    </option>
                  ))}
                </select>
              </label>

              <label className="flex items-center justify-between gap-2 text-xs text-text-secondary">
                <div className="flex flex-col">
                  <span>Safe-Mode (read_only)</span>
                  <span className="text-[10px] text-text-tertiary">
                    PostgreSQL sets default_transaction_read_only = on
                  </span>
                </div>
                <Switch
                  aria-label="db-options-readonly"
                  checked={optionsReadOnly}
                  onCheckedChange={setOptionsReadOnly}
                />
              </label>
            </div>

            <div className="flex justify-end pt-1">
              <Button
                type="button"
                size="sm"
                aria-label="db-options-save"
                onClick={onSaveOptions}
              >
                Save Options
              </Button>
            </div>
          </div>
        ) : null}
      </div>

      {/* Query Console */}
      <div className="db-query flex flex-col gap-2 p-4 rounded-xl bg-surface-raised border border-border">
        <div className="flex items-center justify-between">
          <label htmlFor="db-sql-textarea" className="text-xs font-medium text-text-secondary">
            Query Console
          </label>
          {selectedProfile?.readOnly ? (
            <span className="text-[11px] text-status-warning font-medium">
              Safe-mode active: Writes / DDL will be rejected by PostgreSQL.
            </span>
          ) : null}
        </div>
        <textarea
          id="db-sql-textarea"
          className="h-28 p-3 rounded-lg border border-border bg-surface-base text-xs font-mono text-text-primary outline-none focus:border-status-info resize-y"
          aria-label="db-sql"
          value={sql}
          onChange={(e) => setSql(e.target.value)}
        />
        <div className="flex justify-end">
          <Button type="button" onClick={onRun} disabled={!selectedId}>
            Run query
          </Button>
        </div>
      </div>

      {status ? <p className="db-status text-xs text-status-success">{status}</p> : null}

      {error ? (
        <div
          role="alert"
          className={`p-3 rounded-lg text-xs border ${
            isReadOnlyError
              ? "bg-(--feedback-danger-soft) text-status-danger border-(--border-danger)"
              : "bg-surface-raised text-status-danger border-border"
          }`}
        >
          {isReadOnlyError ? (
            <div className="flex flex-col gap-1">
              <span className="font-semibold">Operation Rejected by Safe-Mode Policy</span>
              <span>{error}</span>
            </div>
          ) : (
            <span>{error}</span>
          )}
        </div>
      ) : null}

      {/* Query Result Area */}
      {result ? (
        <div className="db-result flex flex-col gap-3 p-4 rounded-xl bg-surface-raised border border-border">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-2">
            <div className="flex items-center gap-2">
              <span className="text-xs font-semibold text-text-primary">
                {result.rowCount} row(s)
              </span>
              {result.truncated ? (
                <Badge variant="secondary" className="text-[10px]">
                  Capped at 200 rows
                </Badge>
              ) : null}
            </div>

            <div className="flex items-center gap-2">
              <span className="text-[11px] text-text-tertiary hidden sm:inline">
                JSON exports all values as strings per QueryResult schema.
              </span>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                aria-label="db-export-csv"
                onClick={() => void onExport("csv")}
              >
                Export CSV
              </Button>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                aria-label="db-export-json"
                onClick={() => void onExport("json")}
              >
                Export JSON
              </Button>
            </div>
          </div>

          <div className="overflow-x-auto max-h-96 rounded-lg border border-border">
            <table className="w-full text-left text-xs" aria-label="db-result-table">
              <thead className="bg-surface-base sticky top-0 border-b border-border">
                <tr>
                  {result.columns.map((c) => (
                    <th key={c} className="p-2.5 font-semibold text-text-primary">
                      {c}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-border bg-surface-base">
                {result.rows.map((row, i) => (
                  <tr key={i} className="hover:bg-surface-raised/50 transition-colors">
                    {row.map((cell, j) => (
                      <td key={j} className="p-2.5 text-text-secondary font-mono">
                        {cell}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : null}
    </section>
  );
}
