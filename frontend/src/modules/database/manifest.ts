import { Database } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const databaseModule: ModuleManifest = {
  id: "database",
  title: "Database",
  version: "1.0.0",
  apiVersion: "1",
  icon: Database,
  capabilities: ["db:query"],
  commands: [
    {
      id: "database.query",
      title: "Run query",
    },
  ],
  // A tab per document: a console, a table's data, the ERD, and the three
  // read-only views. views[0] is the module's home — what a Dock click opens.
  tabs: true,
  views: [
    {
      id: "database.console",
      title: "Console",
      ...lazyModuleView(() =>
        import("./DatabaseView").then(({ DatabaseView }) => ({ default: DatabaseView })),
      ),
    },
    {
      id: "database.table",
      title: "Table",
      ...lazyModuleView(() =>
        import("./TableDataView").then(({ TableDataView }) => ({ default: TableDataView })),
      ),
    },
    {
      id: "database.erd",
      title: "ERD diagram",
      ...lazyModuleView(() =>
        import("./components/ERDView").then(({ ERDView }) => ({ default: ERDView })),
      ),
    },
    {
      id: "database.activity",
      title: "Live activity",
      ...lazyModuleView(() =>
        import("./components/LiveActivityView").then(({ LiveActivityView }) => ({ default: LiveActivityView })),
      ),
    },
    {
      id: "database.history",
      title: "Query history",
      ...lazyModuleView(() =>
        import("./components/QueryHistoryView").then(({ QueryHistoryView }) => ({ default: QueryHistoryView })),
      ),
    },
    {
      id: "database.saved",
      title: "Saved queries",
      ...lazyModuleView(() =>
        import("./components/SavedQueriesView").then(({ SavedQueriesView }) => ({ default: SavedQueriesView })),
      ),
    },
  ],
};
