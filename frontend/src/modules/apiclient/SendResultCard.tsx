import { Send } from "lucide-react";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { cn } from "@/lib/utils";
import { dispatchIntent } from "@/workbench/intents";
import type { SendResult } from "./sendResult";

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

/** The status's tone: 2xx went through, 4xx was refused, 5xx broke. */
function statusTone(status: number): string {
  if (status >= 500) return "text-status-danger";
  if (status >= 400) return "text-status-warning";
  if (status >= 200 && status < 300) return "text-status-success";
  return "text-text-primary";
}

/**
 * `api.send` — how the request went: status, time, size. Never the body: the
 * request was really sent, and what came back may be real data.
 */
export function SendResultCard({ response, requestId }: { response: SendResult; requestId: string }) {
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Send}
        title="Response"
        meta={`${response.durationMs} ms`}
        action={
          <FramedCardAction
            onClick={() =>
              void dispatchIntent(
                "api.reveal",
                { module: "apiclient", kind: "request", id: requestId, label: requestId },
                { placement: "beside" },
              )
            }
          >
            Open
          </FramedCardAction>
        }
      />
      <FramedCardPreview className="flex-row items-baseline gap-(--space-inline)">
        <span className={cn("text-code-12-medium tabular-nums", statusTone(response.status))}>{response.status}</span>
        <span className="min-w-0 flex-1 truncate text-body-13 text-text-primary">{response.statusText}</span>
        <span className="shrink-0 text-caption-11 text-text-tertiary tabular-nums">
          {formatBytes(response.bytes)}
          {response.truncated ? " · truncated" : ""}
        </span>
      </FramedCardPreview>
    </FramedCard>
  );
}
