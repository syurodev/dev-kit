import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { SessionSummaryBody, useSessionSummaryData } from "@/modules/agenthub/summary/SessionSummary";
import { environmentRows } from "@/modules/agenthub/summary/summaryModel";
import type { TranscriptItem, Usage } from "@/modules/agenthub/types";

/**
 * The session summary as two vendors fill it: what each one reports decides
 * which sections it has — Claude has background tasks, cost and a plan window;
 * Codex has the token split, two plan windows and its MCP servers. Neither is
 * drawn with a section it has no data for. `?gallery#agenthub-summary`.
 */

const client = new QueryClient();

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

function Card({ vendorName, items, usage, ...rest }: { vendorName: string; items: TranscriptItem[]; usage: Usage } & Pick<Parameters<typeof SessionSummaryBody>[0], "connectors" | "goal">) {
  const data = useSessionSummaryData({ sessionId: "gallery", items, checkpoints: new Map(), live: false });
  const environment = environmentRows({
    vendorName,
    folderPath: "/work/chiro/his-connector",
    workspaceName: "CHIRO",
    repos: [{ name: "his-connector", branch: "feat/receipt-manual-selection", own: true }],
    configOptions: [
      { id: "model", currentValue: "m", options: [{ value: "m", name: vendorName === "Codex" ? "GPT-6.1" : "Opus 5.5" }] },
      { id: "reasoning_effort", currentValue: "medium", options: [{ value: "medium", name: "Medium" }] },
    ],
    mode: "auto",
    availableModes: [{ id: "auto", name: "Auto" }],
  });
  return (
    <div className="w-70 rounded-2xl bg-surface-base py-1.5 shadow-canvas-popover ring-1 ring-border-subtle" data-testid={`summary-${vendorName.toLowerCase()}`}>
      <SessionSummaryBody
        sessionId="gallery"
        checkpoints={new Map()}
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
        <div className="flex flex-wrap items-start gap-6">
          <Card vendorName="Claude" items={claudeItems} usage={claudeUsage} goal={{ objective: "Let a person pick the receipts that count by hand" }} />
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
