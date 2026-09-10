import { useState } from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { copyText } from "../../../bridge/clipboard";
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
import { EmptyState } from "@/components/ui/empty-state";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ListSkeleton } from "@/components/ui/skeleton";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import {
  listSavedQueries,
  deleteSavedQuery,
  type SavedQuery,
} from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";
import { isVaultLockedError } from "../errorStates";

export function SavedQueriesView() {
  const [savedListRef] = useAutoAnimate<HTMLDivElement>({ duration: 180, easing: "cubic-bezier(0.23, 1, 0.32, 1)" });
  const queryClient = useQueryClient();
  const { selectedProfileId, setSql, setActiveTab, setShowSaveQueryModal, activeTab } = useDatabaseStore(
    useShallow((s) => ({
      selectedProfileId: s.selectedProfileId,
      setSql: s.setSql,
      setActiveTab: s.setActiveTab,
      setShowSaveQueryModal: s.setShowSaveQueryModal,
      activeTab: s.activeTab,
    }))
  );

  const [search, setSearch] = useState("");
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [mutationError, setMutationError] = useState("");

  const {
    data: queriesData,
    isPending,
    isFetching,
    error: queryError,
    refetch,
  } = useQuery({
    queryKey: ["database", "saved", selectedProfileId],
    queryFn: () => (selectedProfileId ? listSavedQueries(selectedProfileId) : Promise.resolve([])),
    enabled: Boolean(selectedProfileId) && activeTab === "saved",
    placeholderData: (previousData) => previousData,
  });

  const deleteMutation = useMutation({
    mutationFn: (queryId: string) => deleteSavedQuery(queryId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["database", "saved", selectedProfileId] });
    },
    onError: (e) => {
      setMutationError(errorMessage(e));
    },
  });

  const loading = isFetching || deleteMutation.isPending;
  const error = mutationError || (queryError ? errorMessage(queryError) : "");
  const isVaultLocked = isVaultLockedError(error);
  const queries = isPending && !queriesData ? null : (queriesData ?? []);

  async function handleDelete(queryId: string) {
    setMutationError("");
    await deleteMutation.mutateAsync(queryId);
  }

  function handleCopy(entry: SavedQuery) {
    void copyText(entry.sql);
    setCopiedId(entry.id);
    setTimeout(() => setCopiedId(null), 2000);
  }

  function handleOpenInConsole(entry: SavedQuery) {
    setSql(entry.sql);
    setActiveTab("console");
  }

  const filtered = (queries ?? []).filter(
    (q) =>
      !search ||
      q.name.toLowerCase().includes(search.toLowerCase()) ||
      q.sql.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="flex-1 flex flex-col h-full overflow-hidden select-none">
      {/* Header */}
      {/* No heading: the tab strip directly above already names this view, and
          the same bar height across tabs stops the content jumping on switch. */}
      <div className="px-3 min-h-12 py-2 border-b border-border bg-surface-raised/30 flex flex-wrap items-center justify-between gap-3 shrink-0">
        <div className="flex items-center gap-2">
          <span className="text-body-12 text-text-tertiary">Synced across devices</span>
        </div>

        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="w-3 h-3 absolute left-2.5 top-1/2 -translate-y-1/2 text-text-tertiary" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Filter saved queries…"
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

          <Button
            variant="default"
            size="sm"
            className="h-7 text-label-12 gap-1.5"
            onClick={() => setShowSaveQueryModal(true)}
            disabled={!selectedProfileId}
          >
            <Plus className="w-3.5 h-3.5" />
            <span>Save Current Query</span>
          </Button>
        </div>
      </div>

      {/* Main List */}
      <div className="flex-1 overflow-y-auto scrollbar-gutter-stable p-4 space-y-3">
        {isVaultLocked ? (
          <div className="p-6 rounded-xl border border-border bg-surface-raised/50 text-center flex flex-col items-center justify-center max-w-md mx-auto my-12">
            <Lock className="w-8 h-8 text-amber-500 mb-2" />
            <h3 className="text-title-14 text-text-primary">Vault is Locked</h3>
            <p className="text-body-13 text-text-secondary mt-1">
              Saved queries are encrypted at rest with your vault key. Unlock your vault to access your saved queries.
            </p>
          </div>
        ) : error ? (
          <div className="p-4 rounded-lg bg-status-danger/10 border border-status-danger/30 text-body-12 text-status-danger">
            {error}
          </div>
        ) : queries === null ? (
          <ListSkeleton className="p-0" />
        ) : filtered.length > 0 ? (
          <div ref={savedListRef} className="space-y-3">
            {filtered.map((query) => (
              <div
                key={query.id}
                className="p-3 rounded-lg border border-border bg-surface-raised hover:bg-surface-raised/80 transition-colors flex flex-col gap-2 group"
              >
                <div className="flex items-center justify-between">
                  <span className="text-title-13 text-text-primary">
                    {query.name}
                  </span>

                  <div className="flex items-center gap-1.5">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 px-2 text-caption-11 gap-1 text-text-secondary hover:text-text-primary"
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
                      className="h-6 px-2 text-caption-11 gap-1"
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

                <pre className="text-code-12 text-text-secondary bg-surface-base p-2.5 rounded border border-border/40 overflow-x-auto whitespace-pre-wrap">
                  {query.sql}
                </pre>
              </div>
            ))}
          </div>
        ) : (
          <EmptyState
            icon={Bookmark}
            title="No saved queries yet"
            body="Save reusable statements with custom names to access them anytime."
          />
        )}
      </div>
    </div>
  );
}
