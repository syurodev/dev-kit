import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import type { PendingInteraction } from "@/bridge/interaction";
import { HarnessReviewBody } from "./HarnessReviewBody";
import { HarnessScopeDialog } from "./HarnessScopeDialog";
import WorkspaceHarnessDialog from "./WorkspaceHarnessDialog";
import { WorkspaceBudgetsPanel } from "./WorkspaceBudgetsPanel";
import { AgentHarnessPolicySetting } from "../modules/settings/AgentHarnessPolicySetting";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

test("the dialog offers result limits as a second tab", () => {
  const html = renderToString(
    <WorkspaceHarnessDialog workspace={{ id: "ws-1", name: "Payments" }} onClose={() => {}} portal={false} />,
  );
  expect(html).toContain("Result limits");
  expect(html).toContain('role="tablist"');
  // The limits panel is mounted only once opened, so nothing is read for it yet.
  expect(html).not.toContain("Review limit changes");
});

test("the limits panel explains what a limit is and shows only made-up sample text", () => {
  const html = renderToString(<WorkspaceBudgetsPanel workspaceId="ws-1" active />);
  expect(html).toContain("not tokens or cost");
  expect(html).toContain("Review limit changes");
  expect(html).toContain("made-up sample text");
});

test("the narrowing opt-in is not shown where the controls are not wired", () => {
  const html = renderToString(
    <QueryClientProvider client={new QueryClient()}>
      <AgentHarnessPolicySetting />
    </QueryClientProvider>,
  );
  expect(html).toBe("");
});

const pending: PendingInteraction = {
  id: "p1",
  type: "select",
  kind: "harness_scope",
  env: "",
  tool: "",
  summary:
    "An agent asks to turn on tool groups for its session:\n  • vault (2 tools)\n\nNote from the agent (untrusted text, not checked by DevKit): please",
  allowAlwaysOffered: false,
  caller: "agent:abcdef123456",
  options: [
    { id: "session", label: "This session only", provider: "", subject: "" },
    { id: "workspace", label: "Save to this workspace", provider: "", subject: "" },
  ],
  remember: "",
};

test("the scope choice preselects nothing, has no allow-always and cannot be taken by pressing Enter", () => {
  const html = renderToString(
    <HarnessScopeDialog pending={pending} onCancel={() => {}} onChoose={() => {}} portal={false} />,
  );
  expect(html).toContain("This session only");
  expect(html).toContain("Save to this workspace");
  // Both radios are unchecked and the confirming button is disabled until one is picked.
  expect(html).not.toContain('aria-checked="true"');
  expect(html).toMatch(/<button[^>]*disabled[^>]*>(?:(?!<\/button>).)*Review the change/s);
  expect(html).not.toContain("Allow always");
  // The safe default: Cancel has the focus.
  expect(html).toMatch(/<button[^>]*autofocus[^>]*>(?:(?!<\/button>).)*Cancel/s);
});

test("the agent's own words stay inside the host text under an untrusted label", () => {
  const html = renderToString(
    <HarnessScopeDialog pending={pending} onCancel={() => {}} onChoose={() => {}} portal={false} />,
  );
  expect(html).toContain("untrusted text, not checked by DevKit");
  expect(html).toContain("agent:abcdef123456");
});

test("a review lists the saved configuration and the running session apart", () => {
  const html = renderToString(
    <HarnessReviewBody
      review={{
        id: "r",
        target: "session",
        workspaceId: "w",
        tier: "widening",
        summary: "Turn on the vault tools for this session.",
        needsConfirmation: true,
        changes: [
          { field: "toolGroups", key: "vault", effect: "tool group vault: off → on", scope: "stored" },
          { field: "toolGroups", key: "vault", effect: "tool group vault: unavailable → available", scope: "running" },
        ],
      }}
    />,
  );
  expect(html).toContain("Saved configuration");
  expect(html).toContain("Running session");
  expect(html).toContain("will ask you to confirm");
});

test("the workspace editor names its pinned workspace and edits groups only", () => {
  const html = renderToString(
    <WorkspaceHarnessDialog workspace={{ id: "ws-1", name: "Payments" }} onClose={() => {}} portal={false} />,
  );
  expect(html).toContain("Agent tool groups");
  expect(html).toContain("Payments");
  expect(html).toContain("not tokens");
  expect(html).toContain("Loading tool groups");
  expect(html).toContain("Review changes");
  // No budget, server or skill fields exist in this slice.
  expect(html.toLowerCase()).not.toContain("budget");
  expect(html.toLowerCase()).not.toContain("headtail");
});
