import { Send } from "lucide-react";
import { ToolResultCard } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { SendResult } from "./sendResult";

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

/**
 * ToolResultView for `api.send` (docs/dev-kit/tool-result-view.mdx). Status,
 * timing and size only — never the body, which can carry real response data
 * from a request actually sent. "Open" goes through the existing
 * `api.reveal` intent to the saved request, not the response.
 */
export function SendResultCard({ response, requestId }: { response: SendResult; requestId: string }) {
  const parts = [
    `${response.status} ${response.statusText}`.trim(),
    `${response.durationMs} ms`,
    formatBytes(response.bytes),
  ];
  if (response.truncated) parts.push("truncated");

  return (
    <ToolResultCard
      icon={Send}
      title={parts.join(" · ")}
      actionLabel="Open"
      onAction={() =>
        void dispatchIntent(
          "api.reveal",
          { module: "apiclient", kind: "request", id: requestId, label: requestId },
          { placement: "beside" },
        )
      }
    />
  );
}
