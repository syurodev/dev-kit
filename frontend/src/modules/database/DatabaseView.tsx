import { useEffect, useMemo } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { DEFAULT_CONSOLE_ID, useDatabaseStore } from "./databaseStore";
import { DatabaseSidebar } from "./components/DatabaseSidebar";
import { DatabaseBanners } from "./components/DatabaseBanners";
import { QueryConsole } from "./components/QueryConsole";
import { NewConnectionDialog } from "./components/NewConnectionDialog";
import { ConnectionOptionsDialog } from "./components/ConnectionOptionsDialog";
import { ExportModal } from "./components/ExportModal";
import { SaveQueryModal } from "./components/SaveQueryModal";
import { CellInspector } from "./components/CellInspector";
import { TLSRefusedState, VaultLockedState } from "./components/DatabaseStates";
import { isInsecureTLSError, isVaultLockedError } from "./errorStates";
import { useDatabaseModuleActions } from "./useDatabaseModuleActions";

/**
 * One query console, as a document.
 *
 * The module's header is gone: the profile name, engine and env it restated are
 * all on the rail's selected row already, and its two buttons act on the
 * connection rather than on this tab, so they live in the title bar (see
 * useDatabaseModuleActions). What is left is the console itself.
 */
export function DatabaseView({ params }: { params?: Record<string, unknown> }) {
  const consoleId = typeof params?.consoleId === "string" ? params.consoleId : DEFAULT_CONSOLE_ID;

  const { fetchProfiles, profileError, profiles, selectedProfileId, setShowConnectionOptionsDialog } =
    useDatabaseStore(
      useShallow((s) => ({
        fetchProfiles: s.fetchProfiles,
        profileError: s.profileError,
        profiles: s.profiles,
        selectedProfileId: s.selectedProfileId,
        setShowConnectionOptionsDialog: s.setShowConnectionOptionsDialog,
      })),
    );
  const queryError = useDatabaseStore((s) => s.consoles[consoleId]?.queryError ?? "");

  useEffect(() => {
    void fetchProfiles();
  }, [fetchProfiles]);

  // Carried over from Task 3: re-indexing is a title-bar action on the
  // connection, so its failure is transient and belongs in a toast rather than
  // in a console's error slot.
  const schemaError = useDatabaseStore((s) => s.schemaError);
  useEffect(() => {
    if (schemaError) {
      toast.error("Could not index the schema", { description: schemaError });
    }
  }, [schemaError]);

  const sidebar = useMemo(() => <DatabaseSidebar />, []);
  useSidebarContent(sidebar);
  useDatabaseModuleActions();

  const blocking = profileError || queryError;
  const profile = profiles.find((p) => p.id === selectedProfileId);

  if (isVaultLockedError(profileError)) {
    return <VaultLockedState />;
  }
  if (isInsecureTLSError(blocking)) {
    return (
      <TLSRefusedState
        target={profile ? `${profile.host}:${profile.port}` : "this host"}
        onEditConnection={() => setShowConnectionOptionsDialog(true)}
      />
    );
  }

  return (
    <div className="flex h-full w-full flex-col overflow-hidden text-text-primary">
      <DatabaseBanners />
      <QueryConsole consoleId={consoleId} />
      <NewConnectionDialog />
      <ConnectionOptionsDialog />
      <ExportModal />
      <SaveQueryModal />
      <CellInspector />
    </div>
  );
}

export default DatabaseView;
