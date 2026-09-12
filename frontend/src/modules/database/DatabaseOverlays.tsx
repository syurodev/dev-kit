import { ExportModal } from "./components/ExportModal";
import { SaveQueryModal } from "./components/SaveQueryModal";
import { CellInspector } from "./components/CellInspector";

/**
 * The overlays that belong to a DOCUMENT — its rows, its SQL, one of its cells.
 * The three that belong to the connection are in DatabaseModuleOverlays,
 * rendered by the rail, because they have to work with no document open.
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
      <ExportModal />
      <SaveQueryModal />
      <CellInspector />
    </>
  );
}
