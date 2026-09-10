import { RefreshCw, Settings2 } from "lucide-react";
import { useModuleAction } from "@/stores/moduleActions";
import { useDatabaseStore } from "./databaseStore";

/**
 * The Database module's title-bar actions.
 *
 * The rule these follow, and the reason the old header's buttons are here
 * rather than in a tab: the title bar acts on the CONNECTION, the content
 * toolbar acts on the document in the active tab. Options and re-index are
 * true of the connection no matter which tab is open; Run and Explain are not.
 *
 * "Test connection" is deliberately not here. The rail listing a schema is
 * already proof of a live connection, and a button whose whole output is a
 * chip that disappears after four seconds does not earn a permanent seat.
 */
export function useDatabaseModuleActions(): void {
  const selectedProfileId = useDatabaseStore((s) => s.selectedProfileId);
  const indexingSchema = useDatabaseStore((s) => s.indexingSchema);
  const refreshSchema = useDatabaseStore((s) => s.refreshSchema);
  const setShowConnectionOptionsDialog = useDatabaseStore(
    (s) => s.setShowConnectionOptionsDialog,
  );

  useModuleAction(
    selectedProfileId
      ? {
          id: "database.reindex",
          icon: RefreshCw,
          label: indexingSchema ? "Re-indexing schema…" : "Re-index schema",
          isActive: indexingSchema,
          onClick: () => void refreshSchema(selectedProfileId),
        }
      : null,
  );

  useModuleAction(
    selectedProfileId
      ? {
          id: "database.options",
          icon: Settings2,
          label: "Connection options",
          onClick: () => setShowConnectionOptionsDialog(true),
        }
      : null,
  );
}
