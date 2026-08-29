import { useEffect, useMemo } from "react";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { useDatabaseStore } from "./databaseStore";
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

export function DatabaseView() {
  const { activeTab, fetchProfiles } = useDatabaseStore();

  useEffect(() => {
    void fetchProfiles();
  }, [fetchProfiles]);

  // The shell owns the sidebar rail — its width, its resize handle and its
  // visibility toggle. A module registers content and never lays it out itself,
  // otherwise the rail sits empty while a second panel floats in the content
  // area. DatabaseSidebar reads the store directly, so one element is enough.
  const sidebar = useMemo(() => <DatabaseSidebar />, []);
  useSidebarContent(sidebar);

  return (
    <div className="h-full w-full flex overflow-hidden text-text-primary">
      <main className="flex-1 flex flex-col h-full overflow-hidden min-w-0">
        <DatabaseHeader />
        <DatabaseBanners />

        <div className="flex-1 flex flex-col h-full overflow-hidden min-h-0 relative">
          {activeTab === "console" && <QueryConsole />}
          {activeTab === "erd" && <ERDView />}
          {activeTab === "activity" && <LiveActivityView />}
          {activeTab === "history" && <QueryHistoryView />}
          {activeTab === "saved" && <SavedQueriesView />}
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
