import "@/testing/happyDom";
import { afterEach, beforeEach, expect, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { EMPTY_CHECKPOINTS } from "../checkpoints";
import type { TranscriptItem, Usage } from "../types";
import { resetSummaryFold, setSummarySectionOpen } from "./summaryFold";
import { SessionSummaryBody as Body, useSessionSummaryData, type SessionSummaryInput } from "./SessionSummary";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

type Props = SessionSummaryInput & { usage: Usage | null; turnCount: number };
function SummaryBody(props: Props) {
  return <Body {...props} data={useSessionSummaryData(props)} />;
}

const realNow = Date.now;
const realSetInterval = window.setInterval;
let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  resetSummaryFold();
  setSummarySectionOpen("context", true);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(async () => {
  Date.now = realNow;
  window.setInterval = realSetInterval;
  await act(async () => root.unmount());
  container.remove();
  resetSummaryFold();
});

// A popover left open while a window resets: nothing re-renders it, so the
// section's own clock has to take the window off.
test("a plan window that resets while the summary is open leaves it on the next tick", async () => {
  const t0 = Date.UTC(2026, 9, 29, 12, 0, 0);
  Date.now = () => t0;
  let tick: (() => void) | undefined;
  window.setInterval = ((handler: () => void) => {
    tick = handler;
    return 1;
  }) as unknown as typeof window.setInterval;
  const usage: Usage = {
    used: 10,
    size: 100,
    amount: 0,
    limits: [
      { label: "5-hour", usedPercent: 42, resetsAt: t0 / 1000 + 120 },
      { label: "Weekly", usedPercent: 10, resetsAt: t0 / 1000 + 86_400 },
    ],
  };
  const props = { sessionId: "s", items: [] as TranscriptItem[], checkpoints: EMPTY_CHECKPOINTS, usage, turnCount: 1, live: false };
  await act(async () =>
    root.render(
      <QueryClientProvider client={new QueryClient()}>
        <SummaryBody {...props} />
      </QueryClientProvider>,
    ),
  );
  const labels = () => [...container.querySelectorAll("[data-testid=summary-limit-row]")].map((row) => row.textContent ?? "");
  expect(labels().map((l) => l.split("%")[0])).toEqual(["5-hour42", "Weekly10"]);
  expect(labels()[0]).toContain("resets in 2m");

  Date.now = () => t0 + 121_000;
  await act(async () => tick?.());
  expect(labels().map((l) => l.split("%")[0])).toEqual(["Weekly10"]);
});

// The parent decided to draw the section when the window was still running and
// will not look again until a prop changes, so the section has to withdraw itself.
test("a section that held only a plan window draws nothing once it resets", async () => {
  const t0 = Date.UTC(2026, 9, 29, 12, 0, 0);
  Date.now = () => t0;
  let tick: (() => void) | undefined;
  window.setInterval = ((handler: () => void) => {
    tick = handler;
    return 1;
  }) as unknown as typeof window.setInterval;
  const usage: Usage = { used: 0, size: 0, amount: 0, limits: [{ label: "5-hour", usedPercent: 42, resetsAt: t0 / 1000 + 120 }] };
  const props = { sessionId: "s", items: [] as TranscriptItem[], checkpoints: EMPTY_CHECKPOINTS, usage, turnCount: 1, live: false };
  await act(async () =>
    root.render(
      <QueryClientProvider client={new QueryClient()}>
        <SummaryBody {...props} />
      </QueryClientProvider>,
    ),
  );
  expect(container.querySelector("[data-testid=summary-context]")).not.toBeNull();
  Date.now = () => t0 + 121_000;
  await act(async () => tick?.());
  expect(container.querySelector("[data-testid=summary-context]")).toBeNull();
});

// The 30 s timer is for a window that can still expire: with none left (all
// already reset, or none at all) a mounted card has nothing to count down.
test("the summary starts no clock when no plan window can still expire", async () => {
  const t0 = Date.UTC(2026, 9, 29, 12, 0, 0);
  Date.now = () => t0;
  let started = 0;
  window.setInterval = (() => {
    started++;
    return 1;
  }) as unknown as typeof window.setInterval;
  const render = async (limits: Usage["limits"]) => {
    const props = { sessionId: "s", items: [] as TranscriptItem[], checkpoints: EMPTY_CHECKPOINTS, usage: { used: 10, size: 100, amount: 0, limits }, turnCount: 1, live: false };
    await act(async () =>
      root.render(
        <QueryClientProvider client={new QueryClient()}>
          <SummaryBody {...props} />
        </QueryClientProvider>,
      ),
    );
  };
  await render([{ label: "5-hour", usedPercent: 99, resetsAt: t0 / 1000 - 60 }]);
  await render([]);
  expect(started).toBe(0);
  await render([{ label: "5-hour", usedPercent: 99, resetsAt: t0 / 1000 + 60 }]);
  expect(started).toBe(1);
});
