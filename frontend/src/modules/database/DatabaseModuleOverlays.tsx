import { NewConnectionDialog } from "./components/NewConnectionDialog";
import { ConnectionOptionsDialog } from "./components/ConnectionOptionsDialog";
import { TableDDLDialog } from "./components/TableDDLDialog";

/**
 * The dialogs that belong to the MODULE, not to a document.
 *
 * All six of them used to live in DatabaseOverlays, which a document renders
 * when it is the active tab. That held while a document was always open. It
 * stopped the moment tabs became something remembered rather than invented:
 * with nothing open, no document mounts, so no overlay does — and the rail's
 * "New connection" button set a boolean nothing was listening to. Which is the
 * same report as "Connection options does nothing", from the start of all
 * this, arriving by a different road.
 *
 * These three are opened from the rail and act on a CONNECTION, so they are
 * rendered by the rail — the one thing that mounts exactly once for as long as
 * the module is on screen. The other three act on a document's rows or its SQL
 * and stay with the documents.
 */
export function DatabaseModuleOverlays() {
  return (
    <>
      <NewConnectionDialog />
      <ConnectionOptionsDialog />
      <TableDDLDialog />
    </>
  );
}
