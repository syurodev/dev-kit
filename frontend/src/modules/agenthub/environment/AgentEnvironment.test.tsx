import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { EnvironmentReportView, ReloadSessionControl, reloadBlockedReason } from "./AgentEnvironment";
import type { AgentSession } from "@/bridge/agenthub";
import { ENVIRONMENT_PREVIEW } from "@/dev/AgentEnvironmentPreview";

test("inventory distinguishes discovery, runtime loading and authentication", () => {
  const html = renderToString(<EnvironmentReportView report={ENVIRONMENT_PREVIEW} />);
  expect(html).toContain("Found or configured does not verify execution");
  expect(html).toContain("Loaded");
  expect(html).toContain("Sign-in needed");
  expect(html).toContain("This tool is optional");
  expect(html).toContain("Add a valid name and description");
  expect(html).toContain("after reopening the session");
});

test("a folder granted during the session reads as pending, not as unverified", () => {
  const html = renderToString(<EnvironmentReportView report={{ ...ENVIRONMENT_PREVIEW, scope: "session", entries: [
    { name: "Additional writable folder", kind: "access", status: "pending", source: "/work/api", detail: "Granted. Applies after this session reopens." },
  ] }} />);
  expect(html).toContain("Pending");
  expect(html).toContain("Applies after this session reopens.");
  expect(html).toContain("/work/api");
  expect(html).not.toContain("Unverified");
});

test("next-session report never claims that an agent has started", () => {
  const html = renderToString(<EnvironmentReportView report={{ ...ENVIRONMENT_PREVIEW, scope: "next", entries: [] }} />);
  expect(html).toContain("No agent was started");
  expect(html).toContain("No skills found in the inspected sources");
  expect(html).not.toContain("this session’s launch");
});

test("untrusted names and paths are rendered as text", () => {
  const html = renderToString(<EnvironmentReportView report={{ ...ENVIRONMENT_PREVIEW, entries: [{
    name: "<script>alert(1)</script>", source: "<img src=x onerror=alert(1)>", kind: "skills", status: "made-up", detail: "<b>text</b>",
  }] }} />);
  expect(html).not.toContain("<script>");
  expect(html).not.toContain("<img src=x");
  expect(html).toContain("&lt;script&gt;");
  expect(html).toContain("Unverified");
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AgentCustomizationManager, normalizeCustomization, toggleDisabled } from "./AgentCustomization";

test("customization controls preserve native configuration and expose local management", () => {
  const client = new QueryClient();
  const html = renderToString(<QueryClientProvider client={client}><AgentCustomizationManager vendor="claude" report={ENVIRONMENT_PREVIEW} refresh={() => {}} preview={{ initial: {
    revision: 2, skillFolders: ["/my/skill"], disabledSkills: [], disabledMCP: ["docs"], servers: [{ name: "local", command: "node", enabled: false }],
  } }} /></QueryClientProvider>);
  for (const text of ["Import skill folder", "Remove import", "Add MCP server", "Edit", "Remove", "Existing vendor settings still apply", "server may still connect"]) expect(html).toContain(text);
  expect(html).not.toContain("API_KEY_VALUE");
  expect(html).toContain('aria-label="Allow MCP docs in DevKit"');
});

test("enabling removes only the selected override; disabling deduplicates it", () => {
  expect(toggleDisabled(["a", "b"], "a", true)).toEqual(["b"]);
  expect(toggleDisabled(["a", "b"], "b", false)).toEqual(["a", "b"]);
  expect(normalizeCustomization({ revision: 1 } as never)).toEqual({ revision: 1, skillFolders: [], disabledSkills: [], disabledMCP: [], servers: [] });
});

const reloadWith = (session: Pick<AgentSession, "status" | "activity"> | undefined) =>
  renderToString(<ReloadSessionControl session={session} reloading={false} onReload={() => {}} />);
const disabled = (html: string) => /<button[^>]*\sdisabled=""/.test(html);

test("Reload session waits for a turn, ours or the agent's, and for a permission", () => {
  expect(disabled(reloadWith({ status: "idle" }))).toBe(false);
  expect(disabled(reloadWith({ status: "running" }))).toBe(true);
  expect(disabled(reloadWith({ status: "idle", activity: { agentTurn: true, tasks: 0 } }))).toBe(true);
  expect(disabled(reloadWith({ status: "awaiting_permission" }))).toBe(true);
  expect(reloadBlockedReason({ status: "idle" })).toBe("");
});

test("Reload session does not silently end background tasks, and says why", () => {
  const html = reloadWith({ status: "idle", activity: { agentTurn: false, tasks: 2 } });
  expect(disabled(html)).toBe(true);
  expect(html).toContain("2 background tasks are still running, and reloading would end them.");
  expect(html).toContain('title="2 background tasks are still running');
  const one = reloadBlockedReason({ status: "idle", activity: { agentTurn: false, tasks: 1 } });
  expect(one).toContain("1 background task is still running, and reloading would end it.");
  // A session that only has a revision left has nothing to wait for.
  expect(disabled(reloadWith({ status: "idle", activity: { agentTurn: false, tasks: 0, rev: 3 } }))).toBe(false);
});
