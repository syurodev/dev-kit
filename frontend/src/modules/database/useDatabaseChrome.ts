import { createElement, useEffect, useMemo } from "react";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { getModule } from "@/modules/registry";
import { DatabaseSidebar } from "./components/DatabaseSidebar";
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
 * The module's chrome: the connection rail and the title-bar actions.
 *
 * Every Database document calls this, not just the console. The rail and those
 * actions belong to the CONNECTION, so they have to be there whichever document
 * is on screen — and once the module became several documents, registering them
 * from one of them meant closing that one emptied the rail. A table tab with no
 * console open would have had no rail and no re-index button.
 *
 * Registering from several documents at once is safe by construction: the
 * sidebar store keys content by module id and the action store updates an
 * action in place by its own id, so N callers leave exactly one of each.
 */
export function useDatabaseChrome(): void {
  useSidebarContent(useMemo(() => createElement(DatabaseSidebar), []));
  useDatabaseModuleActions();
  useWarmDocumentChunks();
}
