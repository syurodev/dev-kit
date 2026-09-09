import { useEffect, useMemo, useState } from "react";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { useDatabaseStore, type DatabaseTab } from "./databaseStore";
import { DatabaseSidebar } from "./components/DatabaseSidebar";
import { DatabaseHeader } from "./components/DatabaseHeader";
import { DatabaseBanners } from "./components/DatabaseBanners";
import { QueryConsole } from "./components/QueryConsole";
import { ERDView } from "./components/ERDView";
import { LiveActivityView } from "./components/LiveActivityView";
import { QueryHistoryView } from "./components/QueryHistoryView";
import { SavedQueriesView } from "./components/SavedQueriesView";
import { NewConnectionDialog } from "./components/NewConnectionDialog";
import { ConnectionOptionsDialog } from "./components/ConnectionOptionsDialog";
import { ExportModal } from "./components/ExportModal";
import { SaveQueryModal } from "./components/SaveQueryModal";
import { CellInspector } from "./components/CellInspector";
import { TLSRefusedState, VaultLockedState } from "./components/DatabaseStates";
import { isInsecureTLSError, isVaultLockedError } from "./errorStates";

export function DatabaseView() {
  const {
    activeTab,
    fetchProfiles,
    profileError,
    queryError,
    profiles,
    selectedProfileId,
    setShowConnectionOptionsDialog,
  } = useDatabaseStore();

  useEffect(() => {
    void fetchProfiles();
  }, [fetchProfiles]);

  // The shell owns the sidebar rail — its width, its resize handle and its
  // visibility toggle. A module registers content and never lays it out itself,
  // otherwise the rail sits empty while a second panel floats in the content
  // area. DatabaseSidebar reads the store directly, so one element is enough.
  const sidebar = useMemo(() => <DatabaseSidebar />, []);
  useSidebarContent(sidebar);

  // Two failures deserve a state rather than a line of red text, because both
  // have an obvious next step the error message does not name. They replace
  // the tab content: with the vault shut or the connection refused outright,
  // no tab has anything to show.
  const blocking = profileError || queryError;
  const profile = profiles.find((p) => p.id === selectedProfileId);
  const showVaultLocked = isVaultLockedError(profileError);
  const showTLSRefused = isInsecureTLSError(blocking);

  const [visited, setVisited] = useState<ReadonlySet<DatabaseTab>>(() => new Set([activeTab]));
  useEffect(() => {
    setVisited((prev) => (prev.has(activeTab) ? prev : new Set(prev).add(activeTab)));
  }, [activeTab]);
  const keep = (tab: DatabaseTab, view: React.ReactNode) =>
    tab === activeTab || visited.has(tab) ? (
      <div key={tab} className={tab === activeTab ? "contents" : "hidden"}>
        {view}
      </div>
    ) : null;

  return (
    <div className="h-full w-full flex overflow-hidden text-text-primary">
      <main className="flex-1 flex flex-col h-full overflow-hidden min-w-0">
        <DatabaseHeader />
        <DatabaseBanners />

        <div className="flex-1 flex flex-col h-full overflow-hidden min-h-0 relative">
          {showVaultLocked ? (
            <VaultLockedState />
          ) : showTLSRefused ? (
            <TLSRefusedState
              target={profile ? `${profile.host}:${profile.port}` : "this host"}
              onEditConnection={() => setShowConnectionOptionsDialog(true)}
            />
          ) : (
            <>
          {/* Tabs stay mounted once visited (hidden, like same-module tabs
              in WorkbenchViews): unmount-on-leave meant every return began
              from empty local state and flashed its "nothing here" copy
              before the refetch landed. ERD is the exception — React Flow
              measures itself, and a display:none canvas measures as 0. */}
          {keep("console", <QueryConsole />)}
          {activeTab === "erd" && <ERDView />}
          {keep("activity", <LiveActivityView />)}
          {keep("history", <QueryHistoryView />)}
          {keep("saved", <SavedQueriesView />)}
            </>
          )}
        </div>
      </main>

      {/* Modals and Overlays */}
      <NewConnectionDialog />
      <ConnectionOptionsDialog />
      <ExportModal />
      <SaveQueryModal />
      <CellInspector />
    </div>
  );
}

export default DatabaseView;
