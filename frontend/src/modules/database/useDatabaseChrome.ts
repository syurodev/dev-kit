import { useEffect } from "react";
import { getModule } from "@/modules/registry";
import { useDatabaseModuleActions } from "./useDatabaseModuleActions";

/**
 * Warms every one of the module's view chunks once the module is on screen.
 *
 * Splitting the module into six documents split it into six chunks, and the
 * Suspense boundary is per tab with a whole-module skeleton behind it — so
 * opening each kind of document for the first time repainted the entire module
 * as a skeleton. Measured: the chunk itself arrived in 42ms while the skeleton
 * stayed 313ms, because React throttles removing a fallback it has committed.
 * Making the chunk faster was never the fix; not suspending at all is.
 *
 * Cheap by construction: these are the documents of the module the user is
 * already in, and the split that matters for the initial bundle is the module's
 * own, which has already been paid by the time this runs.
 */
function useWarmDocumentChunks(): void {
  useEffect(() => {
    const views = getModule("database")?.views ?? [];
    for (const view of views) {
      void view.preload?.().catch(() => undefined);
    }
  }, []);
}

/**
 * The module's chrome: the title-bar actions, and the document chunks.
 *
 * The rail is NOT here any more. It was, registered by every Database document
 * because it belongs to the connection rather than to any one of them — and
 * that held right up to the case where no document is open at all, which is
 * now an ordinary state: the workbench remembers what was open instead of
 * inventing a console, and a first launch opens nothing. A rail registered by
 * documents is a rail that disappears with the last of them. It is the
 * manifest's `sidebar` now, which the shell draws for whichever module is on
 * screen.
 */
export function useDatabaseChrome(): void {
  useDatabaseModuleActions();
  useWarmDocumentChunks();
}
