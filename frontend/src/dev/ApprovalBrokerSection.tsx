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

// The pr.* tools that cannot be taken back ask the same way, every time, with
// the exact action and no "allow always". This one is a merge with a failing check.
const mergeSummary = [
  "MERGE PR #412 “Let a person pick the receipts that count by hand” (feat/receipt-manual-selection → main) on github.com/acme/receipts",
  "Method: squash   +318 −47 in 9 file(s)   head 3f9c1ab2",
  "Checks: 3 passed, 1 failing, 2 running",
  "",
  "⚠ Allowing this merges anyway, although:",
  "  • 1 check(s) FAILING: unit (ubuntu)",
  "  • 2 check(s) still running",
  "",
  "This cannot be undone from here.",
].join("\n");
const mergePending = {
  id: "gate-preview-merge", sessionId: session.id, title: session.title, depth: 0,
  params: { origin: "devkit_gate", tool: "pr.merge", env: "", locked: true, summary: mergeSummary,
    toolCall: { title: "pr.merge", kind: "other" },
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
    <div data-testid="gallery-approval-broker-merge" className="w-full max-w-180 rounded-xl border border-border-subtle bg-surface-base p-4">
      <AgentPermissionRequest pending={mergePending} session={session} size="wide" onResolve={(id) => setAnswered(id ? "Allowed in preview" : "Denied in preview")} />
    </div>
  </section>;
}
