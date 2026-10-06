import { ExportModal } from "./components/ExportModal";
import { SaveQueryModal } from "./components/SaveQueryModal";
import { CellInspector } from "./components/CellInspector";

/**
 * The overlays that belong to a DOCUMENT — its rows, its SQL, one of its cells.
 * The three that belong to the connection are in DatabaseModuleOverlays,
 * rendered by the rail, because they have to work with no document open.
 *
 * Render only in the focused document; form snapshots live outside view mounting.
 * Export/Save use shared shadcn dialogs, while CellInspector keeps its existing UI.
 */
export function DatabaseOverlays() {
  return (
    <>
      <ExportModal />
      <SaveQueryModal />
      <CellInspector />
    </>
  );
}
