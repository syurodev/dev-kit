import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { agentHubEnvironment, agentHubResumeSession, agentHubGetStatus, type EnvironmentReport } from "@/bridge/agenthub";
import { errorMessage } from "@/bridge/errors";

import { vendorInfo } from "../vendors";
import { AgentCustomizationManager } from "./AgentCustomization";
import { agentEnvironmentKey } from "./environmentKey";
import { isWorking } from "../sessionActivity";
import { useAgentSessionsStore } from "@/stores/agentSessions";

const GROUPS = [
  { kind: "access", title: "Environment and access", empty: "No access information reported." },
  { kind: "tools", title: "Command-line tools", empty: "No command-line inventory reported." },
  { kind: "skills", title: "Skills", empty: "No skills found in the inspected sources." },
  { kind: "mcp", title: "MCP servers", empty: "No MCP servers found in the inspected sources." },
] as const;

const STATUS: Record<string, string> = {
  found: "Found", configured: "Configured", loaded: "Loaded", reported: "Reported",
  missing: "Not found", unverified: "Unverified", needs_auth: "Sign-in needed", disabled: "Disabled",
  restricted: "Restricted", unavailable: "Unavailable", pending: "Pending",
};

/** Shared by the live dialog, Settings and the bridge-free gallery. */
export function EnvironmentReportView({ report }: { report: EnvironmentReport }) {
  return (
    <div className="flex flex-col gap-5" data-testid="agent-environment-report">
      <p className="text-body-12 text-text-secondary">
        {report.scope === "session"
          ? "Uses this session’s launch environment and grants. Settings changes apply after reopening the session."
          : "Checks the setup for the next session. No agent was started."}
        {" "}Found or configured does not verify execution, connectivity or login.
      </p>
      {GROUPS.map((group) => {
        const entries = report.entries.filter((entry) => entry.kind === group.kind);
        return (
          <section key={group.kind} aria-label={group.title} className="flex flex-col gap-2">
            <h3 className="text-body-13-medium text-text-primary">{group.title}{entries.length ? ` · ${entries.length}` : ""}</h3>
            {!entries.length ? <p className="text-body-12 text-text-tertiary">{group.empty}</p> : (
              <ul className="flex flex-col divide-y divide-border-subtle">
                {entries.map((entry, index) => (
                  <li key={`${entry.name}:${entry.source}:${index}`} className="flex flex-col gap-1 py-2.5 first:pt-0">
                    <div className="flex items-start justify-between gap-3">
                      <span className="min-w-0 break-words text-body-13-medium text-text-primary">{entry.name}</span>
                      <span className={`shrink-0 text-caption-11 ${["needs_auth", "restricted", "unavailable"].includes(entry.status) ? "text-status-warning" : "text-text-tertiary"}`}>
                        {STATUS[entry.status] ?? "Unverified"}
                      </span>
                    </div>
                    <p className="text-body-12 text-text-secondary">{entry.detail}</p>
                    {entry.source ? <p className="break-all text-code-11 text-text-tertiary">{entry.source}</p> : null}
                    {entry.action ? <p className="text-body-12 text-text-primary">{entry.action}</p> : null}
                  </li>
                ))}
              </ul>
            )}
          </section>
        );
      })}
    </div>
  );
}

function EnvironmentCheck({ vendor, sessionId = "", enabled }: { vendor: string; sessionId?: string; enabled: boolean }) {
  const client = useQueryClient();
  const [reloading, setReloading] = useState(false);
  const [reloadError, setReloadError] = useState("");
  const session = useAgentSessionsStore((state) => state.sessionsById[sessionId]);
  async function reload() {
    setReloading(true); setReloadError("");
    try {
      await agentHubResumeSession(sessionId, true);
      useAgentSessionsStore.getState().upsertSession(await agentHubGetStatus(sessionId));
      await client.invalidateQueries({ queryKey: ["agenthub"] });
    } catch (e) { setReloadError(errorMessage(e)); }
    finally { setReloading(false); }
  }
  const query = useQuery({
    queryKey: agentEnvironmentKey(vendor, sessionId),
    queryFn: () => agentHubEnvironment(vendor, sessionId),
    enabled,
    staleTime: 0,
    gcTime: 30_000,
    retry: false,
    refetchOnWindowFocus: false,
  });
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <span className="text-caption-11 text-text-tertiary">{query.data ? `Checked ${new Date(query.data.checkedAt).toLocaleTimeString()}` : "Inspecting environment…"}</span>
        <Button type="button" size="sm" variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>
          {query.isFetching ? <Loader2 className="animate-spin" /> : <RefreshCw />} Refresh
        </Button>
      </div>
      {sessionId ? <div className="flex flex-col gap-2"><Button type="button" size="sm" variant="outline" disabled={reloading || isWorking(session) || session?.status === "awaiting_permission"} onClick={() => void reload()}>{reloading ? <Loader2 className="animate-spin" /> : <RefreshCw />} Reload session</Button><p className="text-body-12 text-text-secondary">Reopens this conversation with current settings without sending a prompt. Finish or stop the current turn first.</p></div> : null}
      {reloadError ? <p role="alert" className="text-body-12 text-status-danger">{reloadError}</p> : null}
      {query.error ? <p role="alert" className="text-body-12 text-status-danger">{errorMessage(query.error)}</p> : null}
      {query.data ? <><EnvironmentReportView report={query.data} />{vendorInfo(vendor).customization ? <AgentCustomizationManager vendor={vendor} report={query.data} refresh={() => { void query.refetch(); }} /> : null}</> : query.isPending ? <p role="status" className="text-body-12 text-text-secondary">Checking tools, skills and MCP configuration…</p> : null}
    </div>
  );
}

/** Settings → Agents → Skills & MCP: one vendor's tools, skills and MCP servers, and DevKit's overrides on them. */
export function AgentEnvironmentPanel({ vendor }: { vendor: string }) {
  const visible = useModuleVisibility();
  return visible ? <EnvironmentCheck vendor={vendor} enabled /> : null;
}

export function AgentEnvironmentDialog({ vendor, sessionId, open, onClose }: { vendor: string; sessionId: string; open: boolean; onClose: () => void }) {
  const visible = useModuleVisibility();
  return (
    <Dialog open={open && visible} onOpenChange={(next) => { if (!next) onClose(); }}>
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Session environment</DialogTitle>
          <DialogDescription>Inspect tools, skills, MCP servers and workspace access without sending a prompt.</DialogDescription>
        </DialogHeader>
        <div className="min-h-0 overflow-y-auto pr-2">
          {open && visible ? <EnvironmentCheck vendor={vendor} sessionId={sessionId} enabled /> : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}
