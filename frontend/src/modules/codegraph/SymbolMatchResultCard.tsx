import { Search } from "lucide-react";
import { ToolResultList, ToolResultRow } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { SymbolMatch } from "./searchResult";

function fileName(path: string): string {
  return path.split("/").pop() || path;
}

const MAX_ROWS = 10;

/**
 * ToolResultView for `codegraph.searchSymbol` (docs/dev-kit/tool-result-view.mdx).
 * Every hit is its own row — reuses the existing `codegraph.reveal` intent
 * (`kind: "node"`), no new registry.
 */
export function SymbolMatchResultCard({ matches }: { matches: SymbolMatch[] }) {
  const shown = matches.slice(0, MAX_ROWS);
  return (
    <ToolResultList icon={Search} title={`${matches.length} match${matches.length === 1 ? "" : "es"}`}>
      {shown.map((match) => (
        <ToolResultRow
          key={match.nodeId}
          label={match.label}
          detail={match.filePath ? `${fileName(match.filePath)}${match.line ? `:${match.line}` : ""}` : undefined}
          onClick={() =>
            void dispatchIntent(
              "codegraph.reveal",
              { module: "codegraph", kind: "node", id: match.nodeId, label: match.label },
              { placement: "beside" },
            )
          }
        />
      ))}
      {matches.length > MAX_ROWS ? (
        <div className="px-3 py-1 text-caption-11 text-text-tertiary/70">+{matches.length - MAX_ROWS} more</div>
      ) : null}
    </ToolResultList>
  );
}
