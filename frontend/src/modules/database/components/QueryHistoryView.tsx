import { useEffect, useState } from "react";
import { copyText } from "../../../bridge/clipboard";
import {
  History,
  Copy,
  Check,
  Terminal,
  RefreshCw,
  Search,
  Lock,
  Calendar,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useDatabaseStore } from "../databaseStore";
import { listQueryHistory, type HistoryEntry } from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";
import { isVaultLockedError } from "../errorStates";

export function QueryHistoryView() {
  const { selectedProfileId, setSql, setActiveTab } = useDatabaseStore();
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [copiedId, setCopiedId] = useState<string | null>(null);

  async function fetchHistory() {
    if (!selectedProfileId) return;
    setLoading(true);
    setError("");
    try {
      const list = await listQueryHistory(selectedProfileId);
      setHistory(list);
    } catch (e) {
      setError(errorMessage(e));
      setHistory([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void fetchHistory();
  }, [selectedProfileId]);

  function handleCopy(entry: HistoryEntry) {
    void copyText(entry.sql);
    setCopiedId(entry.id);
    setTimeout(() => setCopiedId(null), 2000);
  }

  function handleOpenInConsole(entry: HistoryEntry) {
    setSql(entry.sql);
    setActiveTab("console");
  }

  const isVaultLocked = isVaultLockedError(error);

  const filtered = history.filter((h) =>
    !search || h.sql.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="flex-1 flex flex-col h-full overflow-hidden select-none">
      {/* Header */}
      {/* No heading: the tab strip directly above already names this view, and
          the same bar height across tabs stops the content jumping on switch. */}
      <div className="px-3 min-h-12 py-2 border-b border-border bg-surface-raised/30 flex flex-wrap items-center justify-between gap-3 shrink-0">
        <div className="flex items-center gap-2">
          <span className="text-xs text-text-tertiary">Last 50 · local to this device</span>
        </div>

        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="w-3 h-3 absolute left-2.5 top-1/2 -translate-y-1/2 text-text-tertiary" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Filter history…"
              className="h-7 pl-7 text-xs bg-surface-base w-48"
            />
          </div>

          <Button
            variant="outline"
            size="sm"
            className="h-7 text-xs gap-1.5"
            onClick={fetchHistory}
            disabled={loading || !selectedProfileId}
          >
            <RefreshCw className={`w-3 h-3 ${loading ? "animate-spin" : ""}`} />
            <span>Refresh</span>
          </Button>
        </div>
      </div>

      {/* Main Content */}
      <div className="flex-1 overflow-y-auto p-4 space-y-3">
        {isVaultLocked ? (
          <div className="p-6 rounded-xl border border-border bg-surface-raised/50 text-center flex flex-col items-center justify-center max-w-md mx-auto my-12">
            <Lock className="w-8 h-8 text-amber-500 mb-2" />
            <h3 className="text-sm font-semibold text-text-primary">Vault is Locked</h3>
            <p className="text-xs text-text-secondary mt-1 leading-relaxed">
              Query history statements are encrypted at rest with your vault key. Unlock your vault to access statement history.
            </p>
          </div>
        ) : error ? (
          <div className="p-4 rounded-lg bg-status-danger/10 border border-status-danger/30 text-xs text-status-danger">
            {error}
          </div>
        ) : filtered.length > 0 ? (
          filtered.map((entry) => (
            <div
              key={entry.id}
              className="p-3 rounded-lg border border-border bg-surface-raised hover:bg-surface-raised/80 transition-colors flex flex-col gap-2 group"
            >
              <div className="flex items-center justify-between text-[11px] text-text-tertiary">
                <span className="flex items-center gap-1">
                  <Calendar className="w-3 h-3" />
                  {entry.createdAt
                    ? new Date(entry.createdAt).toLocaleString()
                    : "Recent query"}
                </span>

                <div className="flex items-center gap-1.5">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 px-2 text-[11px] gap-1 text-text-secondary hover:text-text-primary"
                    onClick={() => handleCopy(entry)}
                  >
                    {copiedId === entry.id ? (
                      <Check className="w-3 h-3 text-status-success" />
                    ) : (
                      <Copy className="w-3 h-3" />
                    )}
                    <span>{copiedId === entry.id ? "Copied" : "Copy"}</span>
                  </Button>

                  <Button
                    variant="secondary"
                    size="sm"
                    className="h-6 px-2 text-[11px] gap-1"
                    onClick={() => handleOpenInConsole(entry)}
                  >
                    <Terminal className="w-3 h-3" />
                    <span>Open in Console</span>
                  </Button>
                </div>
              </div>

              <pre className="font-mono text-xs text-text-primary bg-surface-base p-2.5 rounded border border-border/40 overflow-x-auto whitespace-pre-wrap">
                {entry.sql}
              </pre>
            </div>
          ))
        ) : (
          <div className="text-center py-16 text-xs text-text-tertiary">
            <History className="w-8 h-8 text-text-tertiary mx-auto mb-2 opacity-50" />
            <p>No queries recorded in history yet.</p>
            <p className="text-[11px] mt-1">Queries executed in the console will automatically appear here.</p>
          </div>
        )}
      </div>
    </div>
  );
}
