import type { ProviderRetry, SessionFailure } from "./types";

/**
 * A retry whose time passed this long ago is not news: the attempt it announced
 * has long since started and either replaced it or cleared it. It is what a
 * reload finds when the app quit mid-outage, and the stored row never heard the
 * turn end.
 */
const STALE_AFTER_MS = 30_000;

/** What the provider said, in one line: its message with the status after it. */
export function providerRetrySentence(retry: ProviderRetry): string {
  const message = (retry.message ?? "").trim().replace(/[.\s]+$/, "");
  if (message && retry.status) return `${message} (${retry.status})`;
  if (message) return message;
  if (retry.status) return `The provider answered ${retry.status}.`;
  return "";
}

/** "in 8 s" until the next attempt, "now" once the time has come. */
export function retryCountdown(retryAt: string | undefined, now: number): string {
  const at = retryAt ? Date.parse(retryAt) : NaN;
  if (Number.isNaN(at)) return "";
  const seconds = Math.ceil((at - now) / 1000);
  return seconds > 0 ? `in ${seconds} s` : "now";
}

/** The banner's detail line: which attempt, when, and what the provider said. */
export function providerRetryDetail(failure: Pick<SessionFailure, "details" | "retryAt" | "revision">, now: number): string {
  const when = retryCountdown(failure.retryAt, now);
  const head = `Retrying, attempt ${failure.revision}${when ? ` ${when}` : ""}`;
  return failure.details ? `${head}: ${failure.details}` : head;
}

/**
 * The retry notice as the session failure the banner already draws — a warning,
 * not a session that stopped, like rateLimitFailure does for a rejected limit.
 * The revision is the attempt, which the banner prints. Null when there is no
 * notice or it is stale.
 */
export function providerRetryFailure(retry: ProviderRetry | null | undefined, now: number = Date.now()): SessionFailure | null {
  if (!retry) return null;
  const at = retry.nextRetryAt ? Date.parse(retry.nextRetryAt) : NaN;
  if (!Number.isNaN(at) && now - at > STALE_AFTER_MS) return null;
  return {
    id: "provider-retry",
    revision: retry.attempt,
    category: "provider_retry",
    severity: "warning",
    title: "The provider is not answering",
    details: providerRetrySentence(retry),
    retryAt: retry.nextRetryAt,
  };
}

/** The rail row's second line while the agent retries. */
export function providerRetryLine(retry: ProviderRetry): string {
  const sentence = providerRetrySentence(retry);
  return `Provider not answering, retrying (attempt ${retry.attempt})${sentence ? `: ${sentence}` : ""}`;
}
