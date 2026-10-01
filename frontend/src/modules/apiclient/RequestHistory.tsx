import { useCallback, useEffect, useState } from "react";
import { History } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  clearRequestHistory,
  requestHistory,
  type ApiHistoryEntry,
} from "@/bridge/apiclient";

/**
 * What this machine sent, and what came back.
 *
 * There is no body and no headers here because there are none stored: either
 * side can carry a token, a body can be megabytes, and re-sending is one
 * click. What is kept is what a person scans a list for.
 *
 * The URL is sealed at rest; a locked vault gives the row back without it,
 * which is a locked vault rather than a broken row and is shown as such.
 */
export function RequestHistory({ requestID }: { requestID: string }) {
  const [entries, setEntries] = useState<ApiHistoryEntry[] | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    if (!requestID) return;
    requestHistory(requestID)
      .then(setEntries)
      .catch((e: Error) => setError(e.message));
  }, [requestID]);

  useEffect(load, [load]);

  if (error) {
    return <p className="text-caption-11 text-status-danger">{error}</p>;
  }
  if (!entries) {
    return null;
  }
  if (entries.length === 0) {
    return <p className="pt-4 text-center text-caption-11 text-text-tertiary">Nothing sent from this machine yet.</p>;
  }

  return (
    <section>
      <header className="flex items-center gap-2 pb-1">
        <History className="size-(--icon-12) text-text-tertiary" aria-hidden />
        <h3 className="flex-1 text-caption-11-medium text-text-tertiary">
          Sent from this machine ({entries.length})
        </h3>
        <Button
          variant="ghost"
          size="xs"
          onClick={() => {
            clearRequestHistory(requestID)
              .then(load)
              .catch((e: Error) => setError(e.message));
          }}
        >
          Clear
        </Button>
      </header>
      <ul>
        {entries.map((entry) => (
          <li
            key={entry.id}
            className="flex items-baseline gap-2 border-b border-border-subtle py-1 text-caption-11 last:border-b-0"
          >
            <span className={statusTone(entry)}>{entry.status || "—"}</span>
            <span className="w-12 shrink-0 font-mono text-text-tertiary">{entry.method}</span>
            <span className="min-w-0 flex-1 truncate font-mono" title={entry.url}>
              {entry.url || "(locked — unlock the vault to read the URL)"}
            </span>
            {entry.failure ? (
              <span className="shrink-0 text-status-warning">{entry.failure}</span>
            ) : (
              <span className="shrink-0 text-text-tertiary">
                {entry.durationMs} ms · {entry.bytes} B
              </span>
            )}
            <span className="shrink-0 text-text-tertiary">{clockOf(entry.sentAt)}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

function statusTone(entry: ApiHistoryEntry): string {
  if (entry.status === 0) return "w-8 shrink-0 text-status-warning";
  if (entry.status >= 500) return "w-8 shrink-0 text-status-danger";
  if (entry.status >= 400) return "w-8 shrink-0 text-status-warning";
  return "w-8 shrink-0 text-status-success";
}

/** Local time, to the minute: a history row answers "when", not "exactly when". */
function clockOf(iso: string): string {
  const at = new Date(iso);
  return Number.isNaN(at.getTime())
    ? ""
    : at.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
