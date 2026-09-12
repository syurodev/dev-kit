import { NewConnectionDialog } from "./components/NewConnectionDialog";
import { ConnectionOptionsDialog } from "./components/ConnectionOptionsDialog";
import { ExportModal } from "./components/ExportModal";
import { SaveQueryModal } from "./components/SaveQueryModal";
import { CellInspector } from "./components/CellInspector";
import { TableDDLDialog } from "./components/TableDDLDialog";

/**
 * The module's six overlays, rendered by whichever document is active.
 *
 * Every one of them is a hand-rolled `fixed inset-0` panel over a `bg-black/60`
 * backdrop, and all five are driven by booleans on the shared store. Rendered
 * per document they would not merely duplicate: three console tabs would stack
 * three backdrops into an opaque screen and three panels would fight for focus.
 * One active tab per module makes the active document the one place to draw
 * them.
 */
export function DatabaseOverlays() {
  return (
    <>
      <NewConnectionDialog />
      <ConnectionOptionsDialog />
      <ExportModal />
      <SaveQueryModal />
      <CellInspector />
      <TableDDLDialog />
    </>
  );
}
