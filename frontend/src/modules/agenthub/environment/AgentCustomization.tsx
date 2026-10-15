import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { agentHubGetCustomization, agentHubSetCustomization, agentHubChooseDirectory, type AgentCustomization, type AgentMCPConfig, type EnvironmentReport } from "@/bridge/agenthub";
import { vendorInfo } from "../vendors";
import { errorMessage } from "@/bridge/errors";

export function normalizeCustomization(value: AgentCustomization): AgentCustomization {
  return { revision: value.revision ?? 0, skillFolders: value.skillFolders ?? [], disabledSkills: value.disabledSkills ?? [], disabledMCP: value.disabledMCP ?? [], servers: value.servers ?? [] };
}
export function toggleDisabled(values: string[], key: string, enabled: boolean): string[] {
  return enabled ? values.filter((value) => value !== key) : [...new Set([...values, key])];
}

/** Saves DevKit overrides; personal and project configuration stays at source. */
type CustomizationPreview = { initial: AgentCustomization };
export function AgentCustomizationManager({ vendor, report, refresh, preview }: { vendor: string; report: EnvironmentReport; refresh: () => void; preview?: CustomizationPreview }) {
  const client = useQueryClient();
  const key = ["agenthub", "customization", vendor];
  const query = useQuery({ queryKey: key, queryFn: async () => normalizeCustomization(await agentHubGetCustomization(vendor)), retry: false, enabled: !preview });
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [editBase, setEditBase] = useState<AgentCustomization | null>(null);
  const [name, setName] = useState("");
  const [target, setTarget] = useState("");
  const [args, setArgs] = useState("[]");
  const [envKeys, setEnvKeys] = useState("");
  const [previewValue, setPreviewValue] = useState(preview?.initial);
  const value = preview ? previewValue : query.data;
  async function save(next: AgentCustomization): Promise<boolean> {
    if (preview) { setPreviewValue(next); setEditing(null); return true; }
    setSaving(true); setError("");
    try {
      const saved = normalizeCustomization(await agentHubSetCustomization(vendor, next));
      client.setQueryData(key, saved);
      await client.invalidateQueries({ queryKey: ["agenthub", "environment"] });
      refresh();
      return true;
    } catch (e) {
      setError(errorMessage(e));
      await query.refetch();
      return false;
    } finally { setSaving(false); }
  }
  async function importSkill() {
    if (!value) return;
    try {
      const folder = preview ? "/Users/developer/skills/local-workflow" : await agentHubChooseDirectory();
      if (folder && !value.skillFolders.includes(folder)) await save({ ...value, skillFolders: [...value.skillFolders, folder] });
    } catch (e) { setError(errorMessage(e)); }
  }
  function edit(server?: AgentMCPConfig) {
    setEditBase(value ?? null);
    setEditing(server?.name ?? ""); setName(server?.name ?? ""); setTarget(server?.url ?? server?.command ?? "");
    setArgs(JSON.stringify(server?.args ?? [])); setEnvKeys((server?.envKeys ?? []).join(", ")); setError("");
  }
  async function saveServer() {
    const base = editBase;
    if (!base) return;
    try {
      const url = /^https?:\/\//i.test(target.trim());
      let parsed: string[] = [];
      if (!url) {
        const candidate: unknown = JSON.parse(args);
        if (!Array.isArray(candidate) || !candidate.every((arg) => typeof arg === "string")) throw new Error("Arguments must be a JSON array of strings.");
        parsed = candidate;
      }
      const server: AgentMCPConfig = { name: name.trim(), enabled: base.servers.find((server) => server.name === editing)?.enabled ?? true, ...(url ? { url: target.trim() } : { command: target.trim(), args: parsed, envKeys: envKeys.split(/[,\s]+/).filter(Boolean) }) };
      if (base.servers.some((item) => item.name === server.name && item.name !== editing)) throw new Error("An MCP server already uses this name.");
      if (await save({ ...base, servers: [...base.servers.filter((item) => item.name !== editing), server] })) setEditing(null);
    } catch (e) { setError(errorMessage(e)); }
  }

  if (!value) return <p role={query.error ? "alert" : "status"} className="text-body-12 text-text-secondary">{query.error ? errorMessage(query.error) : "Loading agent configuration…"}</p>;
  const skills = report.entries.filter((entry) => entry.kind === "skills" && entry.source?.endsWith("SKILL.md") && ["configured", "loaded", "disabled"].includes(entry.status));
  const seenSkills = new Set<string>();
  const servers = [...new Set([...report.entries.filter((entry) => entry.kind === "mcp" && entry.name !== "devkit" && ["configured", "reported", "disabled", "needs_auth"].includes(entry.status)).map((entry) => entry.name), ...value.servers.map((server) => server.name), ...value.disabledMCP])];
  return <section aria-label="Manage skills and MCP" className="flex flex-col gap-4 border-t border-border-subtle pt-4">
    <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-body-13-medium">Manage skills and MCP</h3><Button size="sm" variant="outline" disabled={saving} onClick={() => void importSkill()}>Import skill folder</Button></div>
    <p className="text-body-12 text-text-secondary">Changes apply to new or reloaded sessions. Imported skills use their source folder; update files there, then refresh and reload. Removing an import keeps its files. Existing vendor settings still apply.</p>
    {vendorInfo(vendor).customization === "toolInvocation" ? <p className="text-body-12 text-text-secondary">For inherited Claude skills and MCP, switches control tool invocation. An inherited server may still connect; disable it in its source configuration to stop startup.</p> : null}
    {skills.map((skill) => {
      const path = skill.source!;
      if (seenSkills.has(path)) return null;
      seenSkills.add(path);
      return <div key={path} className="flex items-center gap-3"><div className="min-w-0 flex-1"><span className="text-body-13">{skill.name}</span><p className="break-all text-code-11 text-text-tertiary">{path}</p></div><Switch aria-label={`Allow skill ${skill.name} in DevKit`} disabled={saving} checked={!value.disabledSkills.includes(path)} onCheckedChange={(enabled) => void save({ ...value, disabledSkills: toggleDisabled(value.disabledSkills, path, enabled) })} /></div>;
    })}
    {value.skillFolders.map((folder) => <div key={folder} className="flex items-center gap-2"><span className="min-w-0 flex-1 break-all text-code-11 text-text-secondary">Imported: {folder}</span><Button size="sm" variant="ghost" disabled={saving} onClick={() => void save({ ...value, skillFolders: value.skillFolders.filter((path) => path !== folder), disabledSkills: value.disabledSkills.filter((path) => path !== `${folder}/SKILL.md`) })}>Remove import</Button></div>)}
    <div className="flex items-center justify-between gap-2"><h4 className="text-body-13-medium">MCP servers</h4><Button size="sm" variant="outline" disabled={saving} onClick={() => edit()}>Add MCP server</Button></div>
    {servers.map((serverName) => {
      const custom = value.servers.find((server) => server.name === serverName);
      return <div key={serverName} className="flex items-center gap-2"><span className="min-w-0 flex-1 break-all text-body-13">{serverName}<span className="ml-2 text-caption-11 text-text-tertiary">{custom ? "DevKit" : "Inherited"}</span></span>{custom ? <><Button size="sm" variant="ghost" disabled={saving} onClick={() => edit(custom)}>Edit</Button><Button size="sm" variant="ghost" disabled={saving} onClick={() => void save({ ...value, servers: value.servers.filter((server) => server.name !== serverName), disabledMCP: value.disabledMCP.filter((name) => name !== serverName) })}>Remove</Button></> : null}<Switch aria-label={`Allow MCP ${serverName} in DevKit`} disabled={saving} checked={!value.disabledMCP.includes(serverName) && (custom?.enabled ?? true)} onCheckedChange={(enabled) => void save({ ...value, disabledMCP: toggleDisabled(value.disabledMCP, serverName, enabled), servers: value.servers.map((server) => server.name === serverName ? { ...server, enabled } : server) })} /></div>;
    })}
    {editing !== null ? <form className="flex flex-col gap-3 rounded-control border border-border-subtle p-3" onSubmit={(event) => { event.preventDefault(); void saveServer(); }}>
      <Label className="flex flex-col items-start gap-1">Server name<Input aria-label="Server name" value={name} onChange={(event) => setName(event.target.value)} required /></Label>
      <Label className="flex flex-col items-start gap-1">Command or HTTP URL<Input aria-label="Command or HTTP URL" value={target} onChange={(event) => setTarget(event.target.value)} placeholder="npx or https://example.com/mcp" required /></Label>
      {!/^https?:\/\//i.test(target.trim()) ? <><Label className="flex flex-col items-start gap-1">Arguments (JSON array)<Input aria-label="Arguments" value={args} onChange={(event) => setArgs(event.target.value)} placeholder={'["-y", "my-mcp-server"]'} /></Label><Label className="flex flex-col items-start gap-1">Environment variable names<Input aria-label="Environment variable names" value={envKeys} onChange={(event) => setEnvKeys(event.target.value)} placeholder="GITHUB_TOKEN, API_KEY" /></Label><p className="text-caption-11 text-text-tertiary">Values come from DevKit’s launch environment. This form stores names only.</p></> : null}
      <div className="flex gap-2"><Button type="submit" size="sm" disabled={saving}>Save server</Button><Button type="button" variant="ghost" size="sm" onClick={() => setEditing(null)}>Cancel</Button></div>
    </form> : null}
    {error ? <p role="alert" className="text-body-12 text-status-danger">{error}</p> : null}
  </section>;
}
