import { test, expect } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { MoveReport, ToolReport } from "@/bridge/agentUsageMetrics";
import { initialWindow } from "./measurementModel";
import { measurementOptions } from "./useUsageMeasurements";
import { UsageMeasurementsSetting } from "./UsageMeasurementsSetting";
function render(tools?: ToolReport, moves?: MoveReport, toolError?: Error) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, retryOnMount: false } },
  });
  const { from, to } = initialWindow();
  const q = measurementOptions("claude", from, to, "");
  if (tools) client.setQueryData(q.tools.queryKey, tools);
  if (moves) client.setQueryData(q.moves.queryKey, moves);
  if (toolError)
    client
      .getQueryCache()
      .build(client, q.tools)
      .setState({ status: "error", error: toolError });
  const html = renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <UsageMeasurementsSetting
        vendor="claude"
        accounts={[]}
        accountsError={false}
      />
    </QueryClientProvider>,
  );
  client.clear();
  return html;
}
test("a missing subreport stays loading while recorded zero remains visible", () => {
  const { from, to } = initialWindow();
  const html = render(undefined, {
    vendor: "claude",
    from,
    to,
    nextCursor: "",
    rows: [
      {
        id: "n",
        sessionId: "s",
        seq: 1,
        at: "2026-10-26T09:00:00Z",
        from: "old",
        to: "work",
        toGeneration: "old-generation",
        status: "matched",
        turnId: "t",
        turnStartedAt: "2026-10-26T09:00:01Z",
        cacheWrite: 0,
      },
    ],
  });
  expect(html).toContain("Loading tool measurements");
  expect(html).toContain("First recorded turn");
  expect(html).toContain("removed generation");
  expect(html).toContain(">0</");
  expect(html).toContain("does not establish");
});
test("empty saved measurements differ from an unsaved collector and show coverage warnings", () => {
  const { from, to } = initialWindow();
  const html = render({
    vendor: "claude",
    from,
    to,
    rows: [],
    hasMore: false,
    health: {
      startedAt: "2026-10-26T00:00:00Z",
      savedThrough: "",
      dropped: 5,
      writeFailures: 2,
      uncleanRuns: 1,
    },
  });
  expect(html).toContain("No saved tool measurements");
  expect(html).toContain("not yet saved");
  expect(html).toContain("5");
  expect(html).toContain("unclean runs");
  expect(html).toContain("Loading account moves");
});

test("a failed tool subreport leaves the move table visible", () => {
  const { from, to } = initialWindow();
  const html = render(
    undefined,
    { vendor: "claude", from, to, nextCursor: "", rows: [] },
    new Error("Tool report unavailable"),
  );
  expect(html).toContain("Tool report unavailable");
  expect(html).toContain("No account moves in this window");
  expect(html).not.toContain("No saved tool measurements");
});
