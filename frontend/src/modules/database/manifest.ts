import { runDocumentCommand } from "./documentCommands";
import { Activity, Bookmark, Database, History, Network, Table2, Terminal } from "lucide-react";
import { lazy } from "react";
import { INFRA_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";
import {
  activeConsoleId,
  openActivityTab,
  openErdTab,
  openHistoryTab,
  openNewConsoleTab,
  openSavedTab,
} from "./documents";
import { useDatabaseStore } from "./databaseStore";
import { databaseTabActions } from "./tabActions";

export const databaseModule: ModuleManifest = {
  id: "database",
  title: "Database",
  version: "1.0.0",
  apiVersion: "1",
  icon: Database,
  group: INFRA_GROUP,
  capabilities: ["db:query"],
  // Every one of these is also an item in the native menu bar
  // (internal/appmenu), which knows the id and the shortcut and nothing else.
  // The titles differ by capitalisation on purpose: the menu bar takes
  // title-style, the palette is a search field and takes sentence case.
  commands: [
    { id: "database.console.new", title: "New console", shortcut: "⌘T", run: openNewConsoleTab },
    {
      id: "database.connection.new",
      title: "New connection…",
      shortcut: "⇧⌘N",
      run: () => useDatabaseStore.getState().setShowNewConnectionDialog(true),
    },
    {
      id: "database.query",
      title: "Run query",
      shortcut: "⌘↵",
      // The console owns the keystroke; from the menu it means "run the one in
      // front of you", which is the tab the workbench says is active.
      run: () => runDocumentCommand("run"),
    },
    {
      id: "database.query.cancel",
      title: "Cancel query",
      shortcut: "⌘.",
      run: () => {
        const consoleId = activeConsoleId();
        if (consoleId) useDatabaseStore.getState().cancelQuery(consoleId);
      },
    },
    { id: "database.query.explain", title: "Explain", run: () => runDocumentCommand("explain") },
    {
      id: "database.query.explain-analyze",
      title: "Explain analyze",
      run: () => runDocumentCommand("explainAnalyze"),
    },
    {
      id: "database.query.save",
      title: "Save query…",
      run: () => {
        const consoleId = activeConsoleId();
        const state = useDatabaseStore.getState();
        if (consoleId && state.consoleState(consoleId).sql.trim()) state.openSaveQueryModal(consoleId);
      },
    },
    { id: "database.export", title: "Export…", run: () => runDocumentCommand("export") },
    {
      id: "database.result.close",
      title: "Close result",
      run: () => {
        const consoleId = activeConsoleId();
        if (!consoleId) return;
        const state = useDatabaseStore.getState();
        const slice = state.consoleState(consoleId);
        if (slice.results.length > 0) state.closeResult(consoleId, slice.activeResult);
      },
    },
    { id: "database.table.reload", title: "Reload page", run: () => runDocumentCommand("reload") },
    { id: "database.table.ddl", title: "Show DDL", run: () => runDocumentCommand("showDDL") },
    {
      id: "database.table.open-in-console",
      title: "Open in console",
      run: () => runDocumentCommand("openInConsole"),
    },
    {
      id: "database.schema.reindex",
      title: "Re-index schema",
      shortcut: "⇧⌘R",
      run: () => {
        const state = useDatabaseStore.getState();
        if (state.selectedProfileId) void state.refreshSchema(state.selectedProfileId);
      },
    },
    { id: "database.erd", title: "ERD diagram", shortcut: "⇧⌘D", run: openErdTab },
    { id: "database.activity", title: "Live activity", shortcut: "⇧⌘A", run: openActivityTab },
    { id: "database.history", title: "Query history", shortcut: "⇧⌘Y", run: openHistoryTab },
    { id: "database.saved", title: "Saved queries", shortcut: "⇧⌘B", run: openSavedTab },
  ],
  // A tab per document: a console, a table's data, the ERD, and the three
  // read-only views. views[0] is the module's home — what a Dock click opens.
  tabs: true,
  // The shell's p-4. Every Database view drew its own gutter and they had
  // drifted — 24px in the console, 12px in the three read-only views — so
  // switching tabs moved the content sideways. Nothing here draws a
  // full-bleed divider any more, which is the case the bleed default is for.
  padded: true,
  // The strip's + button. Several consoles at once is the point of the tab
  // strip here, and this is the only affordance that offers one.
  newTab: openNewConsoleTab,
  // What a table or console tab adds to its right-click menu.
  tabActions: databaseTabActions,
  // The connection rail belongs to the module, not to whichever document is
  // open — a table tab and no console still needs it, and so does a module
  // with nothing open at all.
  sidebar: lazy(() =>
    import("./components/DatabaseSidebar").then(({ DatabaseSidebar }) => ({
      default: DatabaseSidebar,
    })),
  ),
  views: [
    {
      id: "database.console",
      icon: Terminal,
      title: "Console",
      ...lazyModuleView(() =>
        import("./DatabaseView").then(({ DatabaseView }) => ({ default: DatabaseView })),
      ),
    },
    {
      id: "database.table",
      icon: Table2,
      title: "Table",
      ...lazyModuleView(() =>
        import("./TableDataView").then(({ TableDataView }) => ({ default: TableDataView })),
      ),
    },
    {
      id: "database.erd",
      icon: Network,
      title: "ERD diagram",
      ...lazyModuleView(() =>
        import("./components/ERDView").then(({ ERDView }) => ({ default: ERDView })),
      ),
    },
    {
      id: "database.activity",
      icon: Activity,
      title: "Live activity",
      ...lazyModuleView(() =>
        import("./components/LiveActivityView").then(({ LiveActivityView }) => ({ default: LiveActivityView })),
      ),
    },
    {
      id: "database.history",
      icon: History,
      title: "Query history",
      ...lazyModuleView(() =>
        import("./components/QueryHistoryView").then(({ QueryHistoryView }) => ({ default: QueryHistoryView })),
      ),
    },
    {
      id: "database.saved",
      icon: Bookmark,
      title: "Saved queries",
      ...lazyModuleView(() =>
        import("./components/SavedQueriesView").then(({ SavedQueriesView }) => ({ default: SavedQueriesView })),
      ),
    },
  ],
  paletteSearch: {
    noun: "tables and connections",
    search: (query, signal) => import("./paletteSearch").then((m) => m.searchPalette(query, signal)),
  },
};
