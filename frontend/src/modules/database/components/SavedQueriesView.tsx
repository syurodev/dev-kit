import { useEffect, useState } from "react";
import {
  Bookmark,
  Plus,
  Terminal,
  Trash2,
  Copy,
  Check,
  RefreshCw,
  Search,
  Lock,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useDatabaseStore } from "../databaseStore";
import {
  listSavedQueries,
  deleteSavedQuery,
  type SavedQuery,
} from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";

export function SavedQueriesView() {
  const { selectedProfileId, setSql, setActiveTab, setShowSaveQueryModal } = useDatabaseStore();
  const [queries, setQueries] = useState<SavedQuery[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [copiedId, setCopiedId] = useState<string | null>(null);

  async function fetchSavedQueries() {
    if (!selectedProfileId) return;
    setLoading(true);
    setError("");
    try {
      const list = await listSavedQueries(selectedProfileId);
      setQueries(list);
    } catch (e) {
      setError(errorMessage(e));
      setQueries([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void fetchSavedQueries();
  }, [selectedProfileId]);

  async function handleDelete(queryId: string) {
    try {
      await deleteSavedQuery(queryId);
      await fetchSavedQueries();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  function handleCopy(entry: SavedQuery) {
    navigator.clipboard.writeText(entry.sql);
    setCopiedId(entry.id);
    setTimeout(() => setCopiedId(null), 2000);
  }

  function handleOpenInConsole(entry: SavedQuery) {
    setSql(entry.sql);
    setActiveTab("console");
  }

  const isVaultLocked =
    error.toLowerCase().includes("vault locked") ||
    error.toLowerCase().includes("locked vault") ||
    error.toLowerCase().includes("vault is locked");

  const filtered = queries.filter(
    (q) =>
      !search ||
      q.name.toLowerCase().includes(search.toLowerCase()) ||
      q.sql.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="flex-1 flex flex-col h-full overflow-hidden bg-surface-base select-none">
      {/* Header */}
      <div className="px-4 py-3 border-b border-border bg-surface-raised/40 flex flex-wrap items-center justify-between gap-3 shrink-0">
        <div className="flex items-center gap-2">
          <Bookmark className="w-4 h-4 text-action-primary" />
          <h2 className="text-sm font-semibold text-text-primary">Saved Queries</h2>
          <span className="text-xs text-text-tertiary">
            (Synced across devices via Vault items)
          </span>
        </div>

        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="w-3 h-3 absolute left-2.5 top-1/2 -translate-y-1/2 text-text-tertiary" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search saved queries…"
              className="h-7 pl-7 text-xs bg-surface-base w-48"
            />
          </div>

          <Button
            variant="outline"
            size="sm"
            className="h-7 text-xs gap-1.5"
            onClick={fetchSavedQueries}
            disabled={loading || !selectedProfileId}
          >
            <RefreshCw className={`w-3 h-3 ${loading ? "animate-spin" : ""}`} />
            <span>Refresh</span>
          </Button>

          <Button
            variant="default"
            size="sm"
            className="h-7 text-xs gap-1.5"
            onClick={() => setShowSaveQueryModal(true)}
            disabled={!selectedProfileId}
          >
            <Plus className="w-3.5 h-3.5" />
            <span>Save Current Query</span>
          </Button>
        </div>
      </div>

      {/* Main List */}
      <div className="flex-1 overflow-y-auto p-4 space-y-3">
        {isVaultLocked ? (
          <div className="p-6 rounded-xl border border-border bg-surface-raised/50 text-center flex flex-col items-center justify-center max-w-md mx-auto my-12">
            <Lock className="w-8 h-8 text-amber-500 mb-2" />
            <h3 className="text-sm font-semibold text-text-primary">Vault is Locked</h3>
            <p className="text-xs text-text-secondary mt-1 leading-relaxed">
              Saved queries are encrypted at rest with your vault key. Unlock your vault to access your saved queries.
            </p>
          </div>
        ) : error ? (
          <div className="p-4 rounded-lg bg-status-danger/10 border border-status-danger/30 text-xs text-status-danger">
            {error}
          </div>
        ) : filtered.length > 0 ? (
          filtered.map((query) => (
            <div
              key={query.id}
              className="p-3 rounded-lg border border-border bg-surface-raised hover:bg-surface-raised/80 transition-colors flex flex-col gap-2 group"
            >
              <div className="flex items-center justify-between">
                <span className="font-semibold text-xs text-text-primary">
                  {query.name}
                </span>

                <div className="flex items-center gap-1.5">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 px-2 text-[11px] gap-1 text-text-secondary hover:text-text-primary"
                    onClick={() => handleCopy(query)}
                  >
                    {copiedId === query.id ? (
                      <Check className="w-3 h-3 text-status-success" />
                    ) : (
                      <Copy className="w-3 h-3" />
                    )}
                    <span>{copiedId === query.id ? "Copied" : "Copy"}</span>
                  </Button>

                  <Button
                    variant="secondary"
                    size="sm"
                    className="h-6 px-2 text-[11px] gap-1"
                    onClick={() => handleOpenInConsole(query)}
                  >
                    <Terminal className="w-3 h-3" />
                    <span>Run in Console</span>
                  </Button>

                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6 text-text-tertiary hover:text-status-danger"
                    onClick={() => void handleDelete(query.id)}
                    title="Delete saved query"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </Button>
                </div>
              </div>

              <pre className="font-mono text-xs text-text-secondary bg-surface-base p-2.5 rounded border border-border/40 overflow-x-auto whitespace-pre-wrap">
                {query.sql}
              </pre>
            </div>
          ))
        ) : (
          <div className="text-center py-16 text-xs text-text-tertiary">
            <Bookmark className="w-8 h-8 text-text-tertiary mx-auto mb-2 opacity-50" />
            <p>No saved queries yet.</p>
            <p className="text-[11px] mt-1">Save reusable statements with custom names to access them anytime.</p>
          </div>
        )}
      </div>
    </div>
  );
}
