import { Send } from "lucide-react";
import { ToolResultRow } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { SendResult } from "./sendResult";

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

/**
 * ToolResultView for `api.send` (docs/dev-kit/tool-result-view.mdx). Status,
 * timing and size only — never the body, which can carry real response data
 * from a request actually sent. The whole row opens the saved request
 * (never the response) through the existing `api.reveal` intent.
 */
export function SendResultCard({ response, requestId }: { response: SendResult; requestId: string }) {
  const size = formatBytes(response.bytes) + (response.truncated ? " · truncated" : "");
  return (
    <ToolResultRow
      icon={Send}
      label={`${response.status} ${response.statusText}`.trim()}
      trailing={`${response.durationMs} ms`}
      detail={size}
      onClick={() =>
        void dispatchIntent(
          "api.reveal",
          { module: "apiclient", kind: "request", id: requestId, label: requestId },
          { placement: "beside" },
        )
      }
    />
  );
}
