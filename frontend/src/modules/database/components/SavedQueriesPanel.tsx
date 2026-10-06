import { FeedbackBanner } from "@/components/ui/feedback";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useQuery } from "@tanstack/react-query";
import { Bookmark, Lock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { SidebarRow } from "@/components/ui/sidebar-shell";
import { ListSkeleton } from "@/components/ui/skeleton";
import { listSavedQueries } from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";
import { isVaultLockedError } from "../errorStates";
import { useDatabaseStore } from "../databaseStore";
import { openSavedQuery, openSavedTab } from "../documents";

/**
 * The saved queries for the connection in front of you.
 *
 * A third list rather than a second section under Consoles, and the reason
 * is what these two things ARE. A console is a local scratch file: written
 * as you type, plaintext, this machine only. A saved query is a vault item:
 * named on purpose, sealed, and synced to every machine you sign in on. One
 * tree holding both would promise they behave alike — drag one into the
 * other, rename either, expect both on your laptop — and none of that is
 * true.
 *
 * Scoped to the selected connection, like the Saved queries document, since
 * a statement saved against one schema is rarely meaningful against another.
 * This is the quick way in; the document is where searching and renaming
 * live, and the footer goes there.
 */
export function SavedQueriesPanel() {
  const active = useWorkbenchTabsStore((s) => s.activeModuleId === "database");
  const selectedProfileId = useDatabaseStore((s) => s.selectedProfileId);

  const { data, isLoading, error, refetch } = useQuery({
    // The same key the Saved queries document uses, so saving in one place
    // refreshes the other without either knowing about it.
    queryKey: ["database", "saved", selectedProfileId],
    queryFn: () =>
      selectedProfileId
        ? listSavedQueries(selectedProfileId)
        : Promise.resolve([]),
    enabled: active && Boolean(selectedProfileId),
  });

  if (!selectedProfileId) {
    return (
      <EmptyState
        size="rail"
        body="Pick a connection to see the queries saved against it."
      />
    );
  }

  if (isLoading) {
    return <ListSkeleton className="px-2 pt-2" />;
  }

  if (error && (!data || isVaultLockedError(errorMessage(error)))) {
    // A locked vault is not a failure to read: the queries are there and
    // sealed, and the answer is the Master Password, not a retry.
    const message = errorMessage(error);
    return isVaultLockedError(message) ? (
      <EmptyState
        size="rail"
        icon={Lock}
        body="Unlock the vault to see saved queries."
      />
    ) : (
      <EmptyState
        size="rail"
        tone="danger"
        title="Could not load saved queries"
        body={message}
      />
    );
  }

  const queries = data ?? [];
  if (queries.length === 0) {
    return (
      <EmptyState
        size="rail"
        title="Nothing saved yet"
        body="Save a console from its toolbar to keep it by name."
      />
    );
  }

  return (
    <>
      {error ? (
        <FeedbackBanner
          tone="error"
          title="Could not refresh saved queries"
          action={
            <Button size="sm" variant="outline" onClick={() => void refetch()}>
              Retry
            </Button>
          }
        >
          {errorMessage(error)}
        </FeedbackBanner>
      ) : null}
      {queries.slice(0, 50).map((entry) => (
        <SidebarRow
          key={entry.id}
          icon={
            <Bookmark className="size-(--icon-12) shrink-0 text-text-tertiary" />
          }
          label={entry.name}
          onClick={() => openSavedQuery(entry)}
        />
      ))}
      <div className="px-3 pt-2">
        <Button
          variant="ghost"
          size="sm"
          className="w-full"
          onClick={() => openSavedTab()}
        >
          Open the full list ({queries.length})
        </Button>
      </div>
    </>
  );
}
