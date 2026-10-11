import { ExportModal } from "./components/ExportModal";
import { SaveQueryModal } from "./components/SaveQueryModal";

/**
 * The overlays that belong to a DOCUMENT — its rows, its SQL, one of its cells.
 * The three that belong to the connection are in DatabaseModuleOverlays,
 * rendered by the rail, because they have to work with no document open.
 *
 * Dialogs use the shared Radix surfaces and are driven by shared store state.
 * Rendered per document they would duplicate: three console tabs would stack
 * three backdrops and three panels would fight for focus.
 * One active tab per module makes the active document the one place to draw
 * them.
 */
export function DatabaseOverlays() {
  return (
    <>
      <ExportModal />
      <SaveQueryModal />
    </>
  );
}
