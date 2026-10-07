import { useState } from "react";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { AgentPermissionRequest } from "@/modules/agenthub/surfaces/AgentPermissionRequest";

const session: AgentSession = { id: "gate-preview", agentType: "codex", projectPath: "/workspace/billing-service", status: "awaiting_permission", parentSessionId: null, depth: 0, title: "Billing service", acpSessionId: "preview" };
const pending = {
  id: "gate-preview-request", sessionId: session.id, title: session.title, depth: 0,
  params: { origin: "devkit_gate", tool: "db.query", env: "prod", locked: true, profileId: "billing-prod", profileName: "Billing Production",
    summary: "SELECT id, statement_code, total_amount, status\nFROM billing.statements\nWHERE statement_code = 'INV-001153'\nLIMIT 10;",
    toolCall: { title: "db.query", kind: "other" },
    options: [{ optionId: "allow", name: "Allow once", kind: "allow_once" }, { optionId: "deny", name: "Deny", kind: "reject_once" }],
  },
} as PendingPermission;

export function ApprovalBrokerSection() {
  const [answered, setAnswered] = useState("");
  return <section id="approval-broker" className="flex flex-col gap-3">
    <h2 className="text-title-16 text-text-primary">Agent Hub — DevKit tool gate</h2>
    <p className="text-body-12 text-text-tertiary">The environment gate asks inside the conversation. Preview only.</p>
    <div data-testid="gallery-approval-broker" className="w-full max-w-180 rounded-xl border border-border-subtle bg-surface-base p-4">
      <AgentPermissionRequest pending={pending} session={session} size="wide" onResolve={(id) => setAnswered(id ? "Allowed in preview" : "Denied in preview")} />
      {answered && <p role="status" className="mt-3 text-body-12 text-text-secondary">{answered}</p>}
    </div>
  </section>;
}
