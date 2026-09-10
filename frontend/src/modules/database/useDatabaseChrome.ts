import { useMemo } from "react";
import { createElement } from "react";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { DatabaseSidebar } from "./components/DatabaseSidebar";
import { useDatabaseModuleActions } from "./useDatabaseModuleActions";

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
}
