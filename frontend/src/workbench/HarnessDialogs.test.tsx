import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import type { PendingInteraction } from "@/bridge/interaction";
import { HarnessReviewBody } from "./HarnessReviewBody";
import { HarnessScopeDialog } from "./HarnessScopeDialog";
import WorkspaceHarnessDialog, { LimitsTabClose } from "./WorkspaceHarnessDialog";
import { WorkspaceBudgetsPanel } from "./WorkspaceBudgetsPanel";
import { AgentHarnessPolicySetting } from "../modules/settings/AgentHarnessPolicySetting";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Review, StaleDiagnosis } from "@/bridge/harness";
import { StaleRepairView } from "./StaleRepairPanel";
import type { StaleState } from "./staleRepairController";

test("the dialog offers result limits as a second tab", () => {
  const html = renderToString(
    <WorkspaceHarnessDialog workspace={{ id: "ws-1", name: "Payments" }} onClose={() => {}} portal={false} />,
  );
  expect(html).toContain("Result limits");
  expect(html).toContain('role="tablist"');
  // The limits panel is mounted only once opened, so nothing is read for it yet.
  expect(html).not.toContain("Review limit changes");
});

test("the dialog hosts the stale-settings repair without a tab or a new surface", () => {
  const html = renderToString(
    <WorkspaceHarnessDialog workspace={{ id: "ws-1", name: "Payments" }} onClose={() => {}} portal={false} />,
  );
  // Nothing is stale before a diagnosis has been read, so the ordinary editor shows.
  expect(html).toContain('data-testid="stale-repair"');
  expect(html).not.toContain("Review repair");
  expect(html).toContain("Loading tool groups");
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

// A list near the 64-entry bound must scroll inside the dialog's height limit with the
// actions outside the scrolling region, so they stay reachable. (Layout structure only:
// the native dialog is AP3 evidence.)
const longDiagnosis = {
  workspaceId: "ws-1", profileRevision: 3, profilePresent: true, state: "stale", total: 64,
  entries: Array.from({ length: 64 }, (_, i) => ({ kind: "group", name: `group-${i}`, on: false })),
} as unknown as StaleDiagnosis;
const staleState = (patch: Partial<StaleState>): StaleState => ({
  workspaceId: "ws-1", diagnosis: longDiagnosis, review: null, outcome: null, busy: "", error: "", readError: "", ...patch,
});
const noop = () => {};
const renderRepair = (state: StaleState) =>
  renderToString(<StaleRepairView state={state} onPrepare={noop} onBack={noop} onApply={noop} onReload={noop} onClose={noop} />);

/** The markup between the scroll region's opening tag and where its own closing div ends. */
function scrollRegion(html: string): string {
  const open = html.indexOf('data-testid="stale-repair-scroll"');
  const actions = html.indexOf('data-testid="stale-repair-actions"');
  expect(open).toBeGreaterThan(-1);
  expect(actions).toBeGreaterThan(open);
  return html.slice(open, actions);
}

test("a long list of stale settings scrolls and keeps Review repair outside the scrolling region", () => {
  const html = renderRepair(staleState({}));
  expect(html).toMatch(/class="[^"]*min-h-0[^"]*overflow-y-auto[^"]*"[^>]*data-testid="stale-repair-scroll"/);
  const inside = scrollRegion(html);
  expect(inside).toContain("group-0");
  expect(inside).toContain("group-63");
  expect(inside).not.toContain("Review repair");
  expect(html.slice(html.indexOf('data-testid="stale-repair-actions"'))).toContain("Review repair");
  // The root can shrink to the dialog's height and fills it only while there is content.
  expect(html).toMatch(/class="[^"]*flex min-h-0 flex-col[^"]*flex-1[^"]*"[^>]*data-testid="stale-repair"/);
});

test("a long review scrolls and keeps Repair and Cancel outside the scrolling region", () => {
  const review = {
    id: "r1", target: "repair", workspaceId: "ws-1", tier: "widening", summary: "Remove 64 stale settings",
    changes: Array.from({ length: 64 }, (_, i) => ({ kind: "group", label: `removes group-${i}` })), needsConfirmation: true,
  } as unknown as Review;
  const html = renderRepair(staleState({ review }));
  const inside = scrollRegion(html);
  expect(inside).not.toContain(">Repair<");
  expect(inside).not.toContain("Cancel");
  const after = html.slice(html.indexOf('data-testid="stale-repair-actions"'));
  expect(after).toContain("Cancel");
  expect(after).toContain("Repair");
});

test("with nothing to repair the panel takes no room from the ordinary editor", () => {
  const html = renderRepair(staleState({ diagnosis: null }));
  expect(html).not.toMatch(/class="[^"]*\bflex-1\b[^"]*"[^>]*data-testid="stale-repair"/);
});

// R1a-2: when the review of a repairable profile would not fit, the panel says so in
// plain words, offers no repair and points at the way out.
test("a repair too long to show in full is explained and not offered", () => {
  const html = renderRepair(staleState({ diagnosis: { ...longDiagnosis, truncated: true } as StaleDiagnosis }));
  expect(html).toContain('data-testid="stale-repair-too-large"');
  expect(html).toContain("too long");
  expect(html).toContain("to show in full");
  expect(html).toContain("restore the missing module or build");
  expect(html).not.toContain("Split");
  // The explanation comes before the list, so it is read without scrolling past 64 entries.
  expect(html.indexOf("stale-repair-too-large")).toBeLessThan(html.indexOf("group-0"));
  expect(html).not.toContain("Repair removes exactly the settings below");
  expect(html).toMatch(/<button[^>]*disabled[^>]*>(<[^>]*>)*Review repair/);
});

// R1a-3: the Result limits tab's Close follows the same rule as every other way out of
// the dialog. While a write is preparing or waiting for the person it is disabled.
test("the limits tab's Close is disabled while a write waits and enabled otherwise", () => {
  const closeButton = (html: string) => html.match(/<button[^>]*>Close<\/button>/)?.[0] ?? "";
  expect(closeButton(renderToString(<LimitsTabClose blocked onClose={noop} />))).toMatch(/\sdisabled(=""|\s|>)/);
  expect(closeButton(renderToString(<LimitsTabClose blocked={false} onClose={noop} />))).not.toMatch(/\sdisabled(=""|\s|>)/);
});

// R1a-4: the shared dialog body inherits the card's max height although the card's
// padding is outside it; letting the body shrink keeps the action row inside the card.
test("the dialog card lets its body shrink inside the card's padding", () => {
  const html = renderToString(
    <WorkspaceHarnessDialog workspace={{ id: "ws-1", name: "Payments" }} onClose={() => {}} portal={false} />,
  );
  const card = html.match(/<[^>]*data-slot="dialog-content"[^>]*>/)?.[0] ?? "";
  expect(card).toContain("data-slot=dialog-body]]:shrink");
});

const groupRequest: PendingInteraction = {
  id: "g1",
  type: "select",
  kind: "harness_group_request",
  env: "",
  tool: "",
  summary:
    "Session 4c686ea3 asks to turn on these tool groups for itself only:\n  • vault (2 tools)\n\nNote from the session (untrusted text, not checked by DevKit): please",
  allowAlwaysOffered: false,
  caller: "agent:4c686ea3",
  options: [
    { id: "approve", label: "Review and allow", provider: "", subject: "" },
    { id: "decline", label: "Decline this request", provider: "", subject: "" },
    { id: "block", label: "Decline and block these groups for this session", provider: "", subject: "" },
  ],
  remember: "",
};

test("a session's request for more groups offers allow, decline and block, preselects nothing and focuses Cancel", () => {
  const html = renderToString(
    <HarnessScopeDialog
      pending={groupRequest}
      title="A session asks for more tool groups"
      choiceLabel="Your answer"
      confirmLabel="Continue"
      onCancel={() => {}}
      onChoose={() => {}}
      portal={false}
    />,
  );
  expect(html).toContain("A session asks for more tool groups");
  expect(html).toContain("Review and allow");
  expect(html).toContain("Decline this request");
  expect(html).toContain("Decline and block these groups for this session");
  expect(html).toContain("untrusted text");
  // Nothing is chosen for the person, there is no allow-always, and Enter lands on Cancel.
  expect(html).not.toContain('aria-checked="true"');
  expect(html).toMatch(/<button[^>]*disabled[^>]*>(?:(?!<\/button>).)*Continue/s);
  expect(html).not.toContain("Allow always");
  expect(html).toMatch(/<button[^>]*autofocus[^>]*>(?:(?!<\/button>).)*Cancel/s);
});

test("the scope question keeps its own wording when no strings are passed", () => {
  const html = renderToString(
    <HarnessScopeDialog pending={pending} onCancel={() => {}} onChoose={() => {}} portal={false} />,
  );
  expect(html).toContain("Where should this be saved?");
  expect(html).toContain("Review the change");
});
