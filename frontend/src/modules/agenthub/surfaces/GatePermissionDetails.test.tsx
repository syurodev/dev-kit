import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentPermissionRequest } from "./AgentPermissionRequest";
import { GatePermissionDetails, gatePermission } from "./GatePermissionDetails";
import { permissionEnv } from "./permissionCard";
import type { PendingPermission } from "@/bridge/agenthub";

test("a gate card shows the full SQL and exact target/environment", () => {
  const summary = `SELECT ${"very_long_column, ".repeat(40)}id FROM orders WHERE id = 12345;`;
  const params = { origin: "devkit_gate", tool: "db.query", summary, profileId: "profile-a", profileName: "Billing", env: "restricted", locked: true };
  expect(gatePermission(params)?.profileId).toBe("profile-a");
  expect(permissionEnv(params)).toBe("locked");
  const html = renderToString(<GatePermissionDetails params={params} />);
  expect(html).toContain(summary);
  expect(html).toContain("Billing");
  expect(html).toContain('title="profile-a"');
  const pending = { id: "gate", sessionId: "session-a", params: { ...params, toolCall: { title: "db.query", rawInput: { command: summary } }, options: [{ optionId: "allow", name: "Allow once", kind: "allow_once" }, { optionId: "deny", name: "Deny", kind: "reject_once" }] } } as PendingPermission;
  const card = renderToString(<TooltipProvider><AgentPermissionRequest pending={pending} onResolve={() => {}} /></TooltipProvider>);
  expect(card).toContain("restricted requires confirmation every time");
  expect(card).not.toContain("Allow always");
  expect(card).toContain(summary);
});

test("vendor permission requests do not become tool gates", () => {
  expect(gatePermission({ tool: "db.query", summary: "SELECT 1" })).toBeNull();
  expect(renderToString(<GatePermissionDetails params={{ origin: "devkit_gate" }} />)).toBe("");
});
