import { Code } from "lucide-react";
import { ToolResultCard } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { EndpointMatch } from "./searchResult";

const MAX_ROWS = 10;

/**
 * ToolResultView for `codegraph.listEndpoints` (docs/dev-kit/tool-result-view.mdx).
 * Every endpoint is its own button — reuses the existing `codegraph.reveal`
 * intent (`kind: "endpoint"`), no new registry.
 */
export function EndpointListResultCard({ endpoints }: { endpoints: EndpointMatch[] }) {
  const shown = endpoints.slice(0, MAX_ROWS);
  return (
    <ToolResultCard icon={Code} title={`${endpoints.length} endpoint${endpoints.length === 1 ? "" : "s"}`}>
      <ul className="flex flex-col gap-0.5">
        {shown.map((endpoint) => (
          <li key={endpoint.id}>
            <button
              type="button"
              onClick={() =>
                void dispatchIntent(
                  "codegraph.reveal",
                  {
                    module: "codegraph",
                    kind: "endpoint",
                    id: endpoint.id,
                    label: `${endpoint.httpMethod} ${endpoint.path}`,
                  },
                  { placement: "beside" },
                )
              }
              className="w-full min-w-0 truncate text-left text-code-12 text-text-tertiary hover:text-text-secondary hover:underline"
            >
              {endpoint.httpMethod} {endpoint.path}
            </button>
          </li>
        ))}
        {endpoints.length > MAX_ROWS ? (
          <li className="text-code-12 text-text-tertiary/70">+{endpoints.length - MAX_ROWS} more</li>
        ) : null}
      </ul>
    </ToolResultCard>
  );
}
