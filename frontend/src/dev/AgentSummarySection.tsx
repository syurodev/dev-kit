import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { setSummarySectionOpen } from "@/modules/agenthub/summary/summaryFold";
import type { CheckpointIndex } from "@/modules/agenthub/checkpoints";

import { SessionSummaryBody, useSessionSummaryData } from "@/modules/agenthub/summary/SessionSummary";
import { environmentRows } from "@/modules/agenthub/summary/summaryModel";
import { pullRequestsView } from "@/modules/agenthub/summary/pullRequestModel";
import type { PullRequestsPanel } from "@/modules/agenthub/summary/usePullRequests";
import type { ChangeRequest, PullRequestItem } from "@/bridge/pullRequests";
import type { TranscriptItem, Usage } from "@/modules/agenthub/types";

/**
 * The session summary as two vendors fill it: what each one reports decides
 * which sections it has — Claude has background tasks, cost and a plan window;
 * Codex has the token split, two plan windows and its MCP servers. Neither is
 * drawn with a section it has no data for. Sections start folded, each with a
 * line saying what is inside; "Open all" / "Fold all" are the gallery's own
 * buttons (the fold is remembered like the app's). The third card is the case
 * that prompted the folding: a workspace of fifteen repositories, long file
 * names, and six repositories that all lack a GitLab account.
 * `?gallery#agenthub-summary`.
 */
const SECTIONS = ["environment", "goal", "plan", "changes", "pullRequests", "background", "sources", "subagents", "connectors", "context"];

const client = new QueryClient();

// What Go reports for a workspace of three repositories: an open request with a
// failing check and a conflict, a merged one, and a repository the account
// cannot reach. The generated types are string enums; the gallery writes the
// strings the wire carries.
const request = (extra: Record<string, unknown>) =>
  ({
    repo: { host: "github.com", path: "acme/receipts" },
    number: 412,
    title: "Let a person pick the receipts that count by hand",
    url: "https://github.com/acme/receipts/pull/412",
    state: "open",
    draft: false,
    head: "feat/receipt-manual-selection",
    base: "main",
    additions: 318,
    deletions: 47,
    changedFiles: 9,
    mergeable: "clean",
    reviewDecision: "",
    checks: { total: 6, passed: 3, failed: 1, running: 2, skipped: 0, failing: [{ name: "unit (ubuntu)", status: "failed", url: "https://github.com/acme/receipts/actions/runs/1", summary: "3 tests failed in ReceiptSelectionTest" }] },
    checksKnown: true,
    unresolvedThreads: 2,
    terms: { platform: "GitHub", short: "PR", long: "pull request", checks: "checks" },
    ...extra,
  }) as unknown as ChangeRequest;
const prItem = (extra: Record<string, unknown>) => ({ folder: "/work/chiro/his-connector", branch: "feat/x", source: "branch", ...extra }) as unknown as PullRequestItem;

const pullRequests: PullRequestsPanel = {
  ...pullRequestsView([
    prItem({ status: "found", changeRequest: request({ mergeable: "conflicting", reviewDecision: "changes_requested" }) }),
    prItem({
      status: "found",
      folder: "/work/chiro/billing",
      changeRequest: request({ number: 98, title: "Round line totals", repo: { host: "github.com", path: "acme/billing" }, state: "merged", additions: 22, deletions: 3, changedFiles: 2 }),
    }),
    prItem({
      status: "no_account",
      folder: "/work/chiro/legacy-ui",
      message: "No GitHub account for this remote (acme/legacy-ui on github.com). Add a token in Accounts.",
    }),
    prItem({
      status: "no_account",
      folder: "/work/chiro/payments",
      platform: "GitLab",
      message: "The GitLab key “id_ed25519_gitlab” pushes your code but can't read merge requests. Add a GitLab token with read_api access in Accounts.",
    }),
  ]),
  attach: async () => {},
  detach: async () => {},
};

const CHIRO_REPOS = ["his-connector", "db-migrations", "log-service", "project-docs", "web-dashboard", "workload", "billing", "gateway", "auth", "notify", "reports", "scheduler", "search", "storage", "ui-kit"];
const chiroItems = CHIRO_REPOS.slice(0, 6).map((name) =>
  prItem({
    status: "no_account",
    folder: `/work/chiro/${name}`,
    message: `No GitLab account for this remote (chiro5893506/${name} on gitlab.com). Add a token in Accounts.`,
  }),
);
const chiroPullRequests: PullRequestsPanel = {
  ...pullRequestsView([
    prItem({
      status: "found",
      folder: "/work/chiro/gateway",
      changeRequest: request({
        repo: { host: "gitlab.com", path: "chiro5893506/gateway" },
        number: 31,
        title: "Cancel bookings in bulk without recalculating",
        url: "https://gitlab.com/chiro5893506/gateway/-/merge_requests/31",
        terms: { platform: "GitLab", short: "MR", long: "merge request", checks: "pipelines" },
        checks: { total: 4, passed: 4, failed: 0, running: 0, skipped: 0 },
        unresolvedThreads: 0,
      }),
    }),
    ...chiroItems,
  ]),
  attach: async () => {},
  detach: async () => {},
};
const chiroChanges = [
  ["src/database/entities/customer.entity.ts", 0, 3],
  ["src/v2/booking/booking-medical-examination-note.spec.ts", 0, 212],
  ["src/v2/booking/booking.service.ts", 2, 21],
  ["src/v2/booking/dto/create-booking.dto.ts", 0, 7],
  ["src/v2/booking/response/booking-detail.response.ts", 0, 7],
  ["Makefile", 4, 0],
] as const;
const chiroCheckpoints = new Map([
  [
    "p1",
    {
      id: "c1",
      createdAt: "2026-10-13T10:00:00Z",
      status: "done",
      changed: chiroChanges.map(([path, added, removed]) => ({ path, kind: "modified", added, removed, binary: false, source: "agent" })),
    },
  ],
]) as unknown as CheckpointIndex;

const plan = {
  id: "plan",
  kind: "plan",
  plan: [
    { content: "Read the receipts code", status: "completed" },
    { content: "Add the manual selection", status: "in_progress" },
    { content: "Run the tests", status: "pending" },
  ],
} as TranscriptItem;

const claudeItems: TranscriptItem[] = [
  plan,
  {
    id: "t1",
    kind: "async_task_spawned",
    asyncTask: { id: "t1", name: "Run the test suite", state: "running", lastToolName: "Bash", usage: { totalTokens: 12_000, toolUses: 4, durationMs: 83_000 } },
  } as TranscriptItem,
  { id: "t2", kind: "async_task_spawned", asyncTask: { id: "t2", name: "Type-check", state: "completed", summary: "No errors" } } as TranscriptItem,
];

const claudeUsage: Usage = {
  used: 186_000,
  size: 1_000_000,
  amount: 2.41,
  currency: "USD",
  limits: [{ label: "Weekly · Opus", usedPercent: 83, resetsAt: Math.floor(Date.now() / 1000) + 26 * 3600, status: "warning" }],
};

const codexUsage: Usage = {
  used: 258_610,
  size: 475_000,
  amount: 0,
  tokens: {
    session: { total: 1_027_261, input: 1_025_338, cached: 1_004_672, output: 1_923, reasoning: 590 },
    lastRequest: { total: 62_000, input: 61_400, cached: 60_100, output: 600, reasoning: 120 },
  },
  limits: [
    { label: "5-hour", usedPercent: 42, resetsAt: Math.floor(Date.now() / 1000) + 3 * 3600, status: "ok" },
    { label: "Weekly", usedPercent: 91, resetsAt: Math.floor(Date.now() / 1000) + 3 * 86400, status: "warning" },
  ],
};

function Card({
  vendorName,
  items,
  usage,
  checkpoints = new Map(),
  repos = [{ name: "his-connector", branch: "feat/receipt-manual-selection", own: true }],
  model,
  ...rest
}: {
  vendorName: string;
  items: TranscriptItem[];
  usage: Usage;
  checkpoints?: CheckpointIndex;
  repos?: { name: string; branch: string; own: boolean }[];
  model?: string;
} & Pick<Parameters<typeof SessionSummaryBody>[0], "connectors" | "goal" | "pullRequests">) {
  const data = useSessionSummaryData({ sessionId: "gallery", items, checkpoints, live: false });
  const environment = environmentRows({
    vendorName,
    folderPath: "/work/chiro/his-connector",
    workspaceName: "CHIRO",
    repos,
    configOptions: [
      { id: "model", currentValue: "m", options: [{ value: "m", name: model ?? (vendorName === "Codex" ? "GPT-6.1" : "Opus 5.5") }] },
      { id: "reasoning_effort", currentValue: "medium", options: [{ value: "medium", name: "Medium" }] },
    ],
    mode: "auto",
    availableModes: [{ id: "auto", name: "Auto" }],
  });
  return (
    <div className="w-70 rounded-2xl bg-surface-base py-1.5 shadow-canvas-popover ring-1 ring-border-subtle" data-testid={`summary-${vendorName.toLowerCase()}`}>
      <SessionSummaryBody
        sessionId="gallery"
        checkpoints={checkpoints}
        usage={usage}
        turnCount={9}
        data={data}
        environment={environment}
        {...rest}
      />
    </div>
  );
}

export function AgentSummarySection() {
  return (
    <QueryClientProvider client={client}>
      <section id="agenthub-summary" className="flex flex-col gap-4 p-6">
        <h2 className="text-title-16 text-text-primary">Agent Hub — session summary by vendor</h2>
        <div className="flex gap-2">
          <Button size="sm" variant="outline" data-testid="summary-open-all" onClick={() => SECTIONS.forEach((name) => setSummarySectionOpen(name, true))}>
            Open all
          </Button>
          <Button size="sm" variant="outline" data-testid="summary-fold-all" onClick={() => SECTIONS.forEach((name) => setSummarySectionOpen(name, false))}>
            Fold all
          </Button>
        </div>
        <div className="flex flex-wrap items-start gap-6">
          <Card vendorName="Claude" items={claudeItems} usage={claudeUsage} goal={{ objective: "Let a person pick the receipts that count by hand" }} pullRequests={pullRequests} />
          <Card
            vendorName="opencode"
            model="opencode/GLM-5.3-Flash Free"
            items={[]}
            usage={{ used: 0, size: 0, amount: 0 }}
            checkpoints={chiroCheckpoints}
            repos={CHIRO_REPOS.map((name, i) => ({ name, branch: i === 3 ? "docs/refresh" : "main", own: i === 0 }))}
            pullRequests={chiroPullRequests}
          />
          <Card
            vendorName="Codex"
            items={[plan]}
            usage={codexUsage}
            connectors={[
              { name: "devkit", state: "ready" },
              { name: "github", state: "failed", error: "401", needsLogin: true },
              { name: "playwright", state: "starting" },
            ]}
          />
        </div>
      </section>
    </QueryClientProvider>
  );
}
