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

// pr.merge, pr.close and pr.deleteBranch ask every time, on a remote that may
// have no environment tag: the card says what is about to happen, offers run
// once or deny, and does not name an environment that is not there.
test("a pull request gate shows the action, offers no allow always and names no environment", () => {
  const summary = "MERGE PR #7 “Add retries” (feat/retries → main) on github.com/acme/widgets\nMethod: squash   +12 −3 in 2 file(s)   head abcdef12\nChecks: 3 passed, 1 failing, 0 running\n\n⚠ Allowing this merges anyway, although:\n  • 1 check(s) FAILING: unit\n\nThis cannot be undone from here.";
  const params = {
    origin: "devkit_gate", tool: "pr.merge", summary, env: "", locked: true,
    toolCall: { title: "pr.merge", kind: "other", rawInput: { command: summary } },
    options: [{ optionId: "allow", name: "Allow once", kind: "allow_once" }, { optionId: "deny", name: "Deny", kind: "reject_once" }],
  };
  expect(gatePermission(params)?.tool).toBe("pr.merge");
  const pending = { id: "gate", sessionId: "session-a", params } as PendingPermission;
  const card = renderToString(<TooltipProvider><AgentPermissionRequest pending={pending} onResolve={() => {}} /></TooltipProvider>);
  for (const want of ["pr.merge", "MERGE PR #7", "1 check(s) FAILING: unit", "This cannot be undone from here.", "Allow once", "Deny", "always asks"]) {
    expect(card).toContain(want);
  }
  expect(card).not.toContain("Allow always");
  expect(card).not.toContain("prod");
});

test("a pull request gate on a tagged remote still names the tag", () => {
  const params = {
    origin: "devkit_gate", tool: "pr.close", summary: "CLOSE PR #7", env: "staging", locked: true,
    options: [{ optionId: "allow", name: "Allow once", kind: "allow_once" }, { optionId: "deny", name: "Deny", kind: "reject_once" }],
  };
  const card = renderToString(<TooltipProvider><AgentPermissionRequest pending={{ id: "g", sessionId: "s", params } as PendingPermission} onResolve={() => {}} /></TooltipProvider>);
  expect(card).toContain("staging requires confirmation every time");
  expect(card).not.toContain("always asks");
});
