import { CodeBlock } from "@/components/code/CodeBlock";
import { cn } from "@/lib/utils";

export function gatePermission(params: unknown) {
  if (!params || typeof params !== "object") return null;
  const value = params as Record<string, unknown>;
  if (value.origin !== "devkit_gate" || typeof value.tool !== "string" || typeof value.summary !== "string") return null;
  return {
    tool: value.tool, summary: value.summary,
    env: typeof value.env === "string" ? value.env : "unknown",
    profileId: typeof value.profileId === "string" ? value.profileId : "",
    profileName: typeof value.profileName === "string" ? value.profileName : "",
    locked: value.locked === true,
  };
}

/** The exact gate request, shared by the inline card and notification surface. */
export function GatePermissionDetails({ params, className, showTool = true }: { params: unknown; className?: string; showTool?: boolean }) {
  const request = gatePermission(params);
  if (!request) return null;
  const sql = /^db\.(query|explain)$/.test(request.tool);
  return <div data-testid="gate-permission-details" className={cn("min-w-0 overflow-hidden rounded-lg border border-border-subtle bg-surface-well", className)}>
    <div className="flex min-w-0 items-center justify-between gap-2 border-b border-border-subtle px-3 py-2 text-body-12">
      <code className="shrink-0 text-text-secondary">{showTool ? request.tool : sql ? "SQL query" : "Request"}</code>
      {request.profileId && <span className="truncate text-text-tertiary" title={request.profileId}>{request.profileName || request.profileId}</span>}
    </div>
    <CodeBlock code={request.summary || "—"} language={sql ? "sql" : request.tool === "ssh.runCommand" ? "bash" : "text"}
      className="max-h-48 overflow-y-auto whitespace-pre-wrap break-words px-3 py-2 text-code-12 leading-5 text-text-primary" />
  </div>;
}
