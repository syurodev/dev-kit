import { describe, expect, it } from "bun:test";
import { renderToString as renderRaw } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";

import { AgentSessionFailure } from "./surfaces/AgentSessionFailure";
import { applySessionUpdate, createInitialTranscriptModel, initTranscript } from "./transcript";
import { providerRetryDetail, providerRetryFailure, providerRetryLine, providerRetrySentence, retryCountdown } from "./providerRetry";
import { retryLines, secondLine } from "./sidebar/model";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import type { ProviderRetry } from "./types";

const render = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);

// opencode's notice as the person's database recorded it during an outage.
const retry: ProviderRetry = {
  attempt: 4,
  nextRetryAt: "2026-10-18T06:28:10.000Z",
  message: "Upstream request failed: Endpoint is unavailable.",
  status: 503,
  type: "provider.internal",
};
const at = Date.parse("2026-10-18T06:28:00.000Z");

describe("the provider's retry notice", () => {
  it("reads as one sentence, with the status", () => {
    expect(providerRetrySentence(retry)).toBe("Upstream request failed: Endpoint is unavailable (503)");
    expect(providerRetrySentence({ attempt: 2, status: 429 })).toBe("The provider answered 429.");
    expect(providerRetrySentence({ attempt: 2 })).toBe("");
  });

  it("counts down to the next attempt", () => {
    expect(retryCountdown(retry.nextRetryAt, at)).toBe("in 10 s");
    expect(retryCountdown(retry.nextRetryAt, at + 9_500)).toBe("in 1 s");
    expect(retryCountdown(retry.nextRetryAt, at + 10_000)).toBe("now");
    expect(retryCountdown(undefined, at)).toBe("");
  });

  it("is a warning banner saying which attempt, when, and what the provider said", () => {
    const failure = providerRetryFailure(retry, at);
    expect(failure).toMatchObject({ id: "provider-retry", revision: 4, severity: "warning", title: "The provider is not answering" });
    expect(providerRetryDetail(failure!, at)).toBe(
      "Retrying, attempt 4 in 10 s: Upstream request failed: Endpoint is unavailable (503)",
    );
  });

  it("is not news once its time is long past: the app quit mid-outage and nothing cleared it", () => {
    expect(providerRetryFailure(retry, at + 10_000 + 29_000)).not.toBeNull();
    expect(providerRetryFailure(retry, at + 10_000 + 31_000)).toBeNull();
    expect(providerRetryFailure(null, at)).toBeNull();
  });

  it("is drawn by the failure banner, with the countdown and without Dismiss", () => {
    const failure = providerRetryFailure(retry, at)!;
    const html = render(<AgentSessionFailure failure={failure} onAction={() => {}} onDismiss={() => {}} />);
    expect(html).toContain("The provider is not answering");
    expect(html).toContain('data-severity="warning"');
    expect(html).toContain("Retrying, attempt 4");
    expect(html).toContain("Upstream request failed: Endpoint is unavailable (503)");
    // The next attempt or the turn ending replaces it; a dismissed one would hide the next outage.
    expect(html).not.toContain("session-failure-dismiss");
  });
});

describe("the retry notice in the transcript", () => {
  it("is replaced on each attempt and cleared by a null, live", () => {
    let model = createInitialTranscriptModel();
    model = applySessionUpdate(model, { kind: "provider_retry_update", retry });
    expect(model.retry?.attempt).toBe(4);
    model = applySessionUpdate(model, { kind: "provider_retry_update", retry: { ...retry, attempt: 5 } });
    expect(model.retry?.attempt).toBe(5);
    model = applySessionUpdate(model, { kind: "provider_retry_update", retry: null });
    expect(model.retry).toBeNull();
    expect(model.items).toHaveLength(0);
  });

  it("outlives a new prompt (its one stored row sits above that prompt) but not the turn", () => {
    let model = createInitialTranscriptModel();
    model = applySessionUpdate(model, { kind: "provider_retry_update", retry });
    model = applySessionUpdate(model, { kind: "user_message_chunk", content: { type: "text", text: "again" } });
    expect(model.retry?.attempt).toBe(4);
  });

  it("arrives at the same state on a reload", () => {
    const row = (kind: string, payload: object, seq: number) => ({ id: `r${seq}`, kind, seq, payload: JSON.stringify(payload) });
    const during = initTranscript([
      row("user_message_chunk", { kind: "user_message_chunk", content: { type: "text", text: "go" } }, 1),
      row("provider_retry_update", { kind: "provider_retry_update", retry }, 2),
    ]);
    expect(during.retry?.attempt).toBe(4);
    expect(during.items).toHaveLength(1);

    const after = initTranscript([
      row("user_message_chunk", { kind: "user_message_chunk", content: { type: "text", text: "go" } }, 1),
      row("provider_retry_update", { kind: "provider_retry_update" }, 2),
    ]);
    expect(after.retry).toBeNull();
  });
});

describe("the retry notice in the rail", () => {
  it("is the row's second line while it lasts, in the warning tone, and an ask still wins", () => {
    expect(providerRetryLine(retry)).toBe("Provider not answering, retrying (attempt 4): Upstream request failed: Endpoint is unavailable (503)");
    const lines = retryLines({ s1: retry });
    expect(secondLine({ session: { status: "running", preview: "old preview" }, ask: lines.s1 })).toEqual({
      text: providerRetryLine(retry),
      tone: "warning",
    });
    expect(secondLine({ session: { status: "running", preview: "old preview" }, ask: null }).text).toBe("old preview");
  });

  it("is kept per session in the store and cleared with null", () => {
    const store = useAgentSessionsStore.getState();
    store.setProviderRetry("s1", retry);
    expect(useAgentSessionsStore.getState().providerRetries.s1?.attempt).toBe(4);
    store.setProviderRetry("s1", null);
    expect(useAgentSessionsStore.getState().providerRetries.s1).toBeUndefined();
  });
});
