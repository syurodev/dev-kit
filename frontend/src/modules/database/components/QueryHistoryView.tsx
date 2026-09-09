import { useState } from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { useQuery } from "@tanstack/react-query";
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
import { ListSkeleton } from "@/components/ui/skeleton";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import { listQueryHistory, type HistoryEntry } from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";
import { isVaultLockedError } from "../errorStates";

export function QueryHistoryView() {
  const [historyListRef] = useAutoAnimate<HTMLDivElement>({ duration: 180, easing: "ease-out" });
  const { selectedProfileId, setSql, setActiveTab, activeTab } = useDatabaseStore(
    useShallow((s) => ({
      selectedProfileId: s.selectedProfileId,
      setSql: s.setSql,
      setActiveTab: s.setActiveTab,
      activeTab: s.activeTab,
    }))
  );

  const [search, setSearch] = useState("");
  const [copiedId, setCopiedId] = useState<string | null>(null);

  const {
    data: historyData,
    isPending,
    isFetching,
    error: queryError,
    refetch,
  } = useQuery({
    queryKey: ["database", "history", selectedProfileId],
    queryFn: () => (selectedProfileId ? listQueryHistory(selectedProfileId) : Promise.resolve([])),
    enabled: Boolean(selectedProfileId) && activeTab === "history",
    placeholderData: (previousData) => previousData,
  });

  const loading = isFetching;
  const error = queryError ? errorMessage(queryError) : "";
  const isVaultLocked = isVaultLockedError(error);
  const history = isPending && !historyData ? null : (historyData ?? []);

  function handleCopy(entry: HistoryEntry) {
    void copyText(entry.sql);
    setCopiedId(entry.id);
    setTimeout(() => setCopiedId(null), 2000);
  }

  function handleOpenInConsole(entry: HistoryEntry) {
    setSql(entry.sql);
    setActiveTab("console");
  }

  const filtered = (history ?? []).filter((h) =>
    !search || h.sql.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="flex-1 flex flex-col h-full overflow-hidden select-none">
      {/* Header */}
      {/* No heading: the tab strip directly above already names this view, and
          the same bar height across tabs stops the content jumping on switch. */}
      <div className="px-3 min-h-12 py-2 border-b border-border bg-surface-raised/30 flex flex-wrap items-center justify-between gap-3 shrink-0">
        <div className="flex items-center gap-2">
          <span className="text-body-12 text-text-tertiary">Last 50 · local to this device</span>
        </div>

        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="w-3 h-3 absolute left-2.5 top-1/2 -translate-y-1/2 text-text-tertiary" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Filter history…"
              className="h-7 pl-7 text-body-12 bg-surface-base w-48"
            />
          </div>

          <Button
            variant="outline"
            size="sm"
            className="h-7 text-label-12 gap-1.5"
            onClick={() => void refetch()}
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
            <h3 className="text-title-14 text-text-primary">Vault is Locked</h3>
            <p className="text-body-13 text-text-secondary mt-1">
              Query history statements are encrypted at rest with your vault key. Unlock your vault to access statement history.
            </p>
          </div>
        ) : error ? (
          <div className="p-4 rounded-lg bg-status-danger/10 border border-status-danger/30 text-body-12 text-status-danger">
            {error}
          </div>
        ) : history === null ? (
          <ListSkeleton className="p-0" />
        ) : filtered.length > 0 ? (
          <div ref={historyListRef} className="space-y-3">
            {filtered.map((entry) => (
              <div
                key={entry.id}
                className="p-3 rounded-lg border border-border bg-surface-raised hover:bg-surface-raised/80 transition-colors flex flex-col gap-2 group"
              >
                <div className="flex items-center justify-between text-caption-11 text-text-tertiary">
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
                      className="h-6 px-2 text-caption-11 gap-1 text-text-secondary hover:text-text-primary"
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
                      className="h-6 px-2 text-caption-11 gap-1"
                      onClick={() => handleOpenInConsole(entry)}
                    >
                      <Terminal className="w-3 h-3" />
                      <span>Open in Console</span>
                    </Button>
                  </div>
                </div>

                <pre className="text-code-12 text-text-primary bg-surface-base p-2.5 rounded border border-border/40 overflow-x-auto whitespace-pre-wrap">
                  {entry.sql}
                </pre>
              </div>
            ))}
          </div>
        ) : (
          <div className="text-center py-16 text-body-12 text-text-tertiary">
            <History className="w-8 h-8 text-text-tertiary mx-auto mb-2 opacity-50" />
            <p>No queries recorded in history yet.</p>
            <p className="text-caption-11 mt-1">Queries executed in the console will automatically appear here.</p>
          </div>
        )}
      </div>
    </div>
  );
}
