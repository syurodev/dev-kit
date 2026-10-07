import { useState } from "react";
import { Button } from "@/components/ui/button";
import { MCPInteractionDialog } from "@/workbench/MCPInteractionDialog";
import type { PendingInteraction } from "@/bridge/interaction";

const request: PendingInteraction = {
  id: "preview-request", type: "confirm", kind: "database", env: "prod", tool: "db.query",
  summary: "SELECT bs.id, bs.statement_code, bs.total_amount, bs.discount_amount,\n       bs.status, bs.created_at, bs.updated_at\nFROM billing.statements bs\nWHERE bs.statement_code = 'INV-001153'\nLIMIT 10;",
  caller: "agent:da4c04e2-dd94-40e0-81aa-7ef34c94227d", options: [], remember: "", allowAlwaysOffered: false,
};

export function MCPInteractionSection() {
  const [kind, setKind] = useState<"" | "query" | "schema" | "profile">("");
  const [selectedId, setSelectedId] = useState("prod");
  return <section id="mcp-interactions" className="flex flex-col gap-3">
    <h2 className="text-title-16 text-text-primary">MCP — connection and approval</h2>
    <div className="flex flex-wrap gap-2">
      <Button variant="outline" onClick={() => setKind("profile")}>Preview profile selection</Button>
      <Button variant="outline" onClick={() => setKind("schema")}>Preview schema request</Button>
      <Button variant="outline" onClick={() => setKind("query")}>Preview SQL request</Button>
    </div>
    {kind && <MCPInteractionDialog key={kind}
      pending={kind === "profile" ? { ...request, type: "select" } : kind === "schema" ? { ...request, tool: "db.schema.get", summary: "Read the schema index for this database." } : request}
      callerName="Billing service" callerVendor="codex" waiting={1}
      profiles={[
        { id: "prod", name: "Billing Production", env: "prod", detail: "billing · db.internal:5432" },
        { id: "staging", name: "Billing Staging", env: "stg", detail: "billing_staging · staging.internal:5432" },
        { id: "local", name: "Local development", env: "dev", detail: "billing · localhost:5432" },
      ]}
      selectedId={selectedId} onSelect={setSelectedId} onDeny={() => setKind("")} onAllow={() => setKind("")} />}
  </section>;
}
