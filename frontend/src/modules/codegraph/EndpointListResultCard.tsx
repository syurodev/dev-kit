import { Code } from "lucide-react";
import { ToolResultList, ToolResultRow } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { EndpointMatch } from "./searchResult";

const MAX_ROWS = 10;

/**
 * ToolResultView for `codegraph.listEndpoints` (docs/dev-kit/tool-result-view.mdx).
 * Every endpoint is its own row — reuses the existing `codegraph.reveal`
 * intent (`kind: "endpoint"`), no new registry.
 */
export function EndpointListResultCard({ endpoints }: { endpoints: EndpointMatch[] }) {
  const shown = endpoints.slice(0, MAX_ROWS);
  return (
    <ToolResultList icon={Code} title={`${endpoints.length} endpoint${endpoints.length === 1 ? "" : "s"}`}>
      {shown.map((endpoint) => (
        <ToolResultRow
          key={endpoint.id}
          label={
            <>
              <span className="text-code-12-medium">{endpoint.httpMethod}</span> {endpoint.path}
            </>
          }
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
        />
      ))}
      {endpoints.length > MAX_ROWS ? (
        <div className="px-3 py-1 text-caption-11 text-text-tertiary/70">+{endpoints.length - MAX_ROWS} more</div>
      ) : null}
    </ToolResultList>
  );
}
