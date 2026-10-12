import { DatabaseDocumentLayout } from "./components/DatabaseDocumentLayout";
import { useShallow } from "zustand/react/shallow";
import { DEFAULT_CONSOLE_ID, useDatabaseStore } from "./databaseStore";
import { DatabaseBanners } from "./components/DatabaseBanners";
import { QueryConsole } from "./components/QueryConsole";
import { DatabaseOverlays } from "./DatabaseOverlays";
import { TLSRefusedState, VaultLockedState } from "./components/DatabaseStates";
import { isInsecureTLSError, isVaultLockedError } from "./errorStates";
import { useDatabaseChrome } from "./useDatabaseChrome";
import { useConsoleLifetime } from "./useConsoleLifetime";
import { useIsActiveDocument } from "./useIsActiveDocument";

/**
 * One query console, as a document.
 *
 * The module's header is gone: the profile name, engine and env it restated are
 * all on the rail's selected row already, and its two buttons act on the
 * connection rather than on this tab, so they live in the title bar (see
 * useDatabaseModuleActions). What is left is the console itself.
 */
export function DatabaseView({ params }: { params?: Record<string, unknown> }) {
  const consoleId =
    typeof params?.consoleId === "string"
      ? params.consoleId
      : DEFAULT_CONSOLE_ID;

  const {
    profileError,
    profiles,
    selectedProfileId,
    setShowConnectionOptionsDialog,
  } = useDatabaseStore(
    useShallow((s) => ({
      profileError: s.profileError,
      profiles: s.profiles,
      selectedProfileId: s.selectedProfileId,
      setShowConnectionOptionsDialog: s.setShowConnectionOptionsDialog,
    })),
  );
  const queryError = useDatabaseStore(
    (s) => s.consoles[consoleId]?.queryError ?? "",
  );
  // The tab's own params, not the normalised consoleId: the module's home tab
  // is opened by the Workbench with no params at all, so asking for
  // `consoleId=console-1` names a tab that does not exist and the overlays
  // never mount — which is what made Connection options do nothing on a fresh
  // module visit, from the menu and the row button alike.
  const isActive = useIsActiveDocument("database.console", params);

  useDatabaseChrome();
  useConsoleLifetime(consoleId);

  const boundTarget = useDatabaseStore(
    (s) => s.consoles[consoleId]?.targetProfileId,
  );
  const targetId = boundTarget ?? selectedProfileId;
  const blocking =
    (targetId === selectedProfileId ? profileError : "") || queryError;
  const profile = profiles.find((p) => p.id === targetId);

  if (isVaultLockedError(profileError)) {
    return <VaultLockedState />;
  }
  if (isInsecureTLSError(blocking)) {
    return (
      <TLSRefusedState
        target={profile ? `${profile.host}:${profile.port}` : "this host"}
        onEditConnection={() => {
          useDatabaseStore
            .getState()
            .setSelectedProfileId(profile?.parentProfileId || targetId);
          setShowConnectionOptionsDialog(true);
        }}
      />
    );
  }

  return (
    <div className="flex h-full w-full flex-col overflow-hidden text-text-primary">
      <DatabaseBanners profileId={targetId} />
      <DatabaseDocumentLayout active={isActive}>
        <QueryConsole consoleId={consoleId} />
      </DatabaseDocumentLayout>
      {isActive ? <DatabaseOverlays /> : null}
    </div>
  );
}

export default DatabaseView;
