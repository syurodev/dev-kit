import { Search } from "lucide-react";
import { ToolResultCard } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { SymbolMatch } from "./searchResult";

function fileName(path: string): string {
  return path.split("/").pop() || path;
}

const MAX_ROWS = 10;

/**
 * ToolResultView for `codegraph.searchSymbol` (docs/dev-kit/tool-result-view.mdx).
 * Every hit is its own button — reuses the existing `codegraph.reveal`
 * intent (`kind: "node"`), no new registry.
 */
export function SymbolMatchResultCard({ matches }: { matches: SymbolMatch[] }) {
  const shown = matches.slice(0, MAX_ROWS);
  return (
    <ToolResultCard icon={Search} title={`${matches.length} match${matches.length === 1 ? "" : "es"}`}>
      <ul className="flex flex-col gap-0.5">
        {shown.map((match) => (
          <li key={match.nodeId}>
            <button
              type="button"
              onClick={() =>
                void dispatchIntent(
                  "codegraph.reveal",
                  { module: "codegraph", kind: "node", id: match.nodeId, label: match.label },
                  { placement: "beside" },
                )
              }
              className="w-full min-w-0 truncate text-left text-code-12 text-text-tertiary hover:text-text-secondary hover:underline"
            >
              {match.label}
              {match.filePath ? (
                <span className="text-text-tertiary/70">
                  {" "}
                  — {fileName(match.filePath)}
                  {match.line ? `:${match.line}` : ""}
                </span>
              ) : null}
            </button>
          </li>
        ))}
        {matches.length > MAX_ROWS ? (
          <li className="text-code-12 text-text-tertiary/70">+{matches.length - MAX_ROWS} more</li>
        ) : null}
      </ul>
    </ToolResultCard>
  );
}
