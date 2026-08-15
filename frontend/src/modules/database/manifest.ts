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
  views: [
    {
      id: "database.connections",
      title: "Database",
      ...lazyModuleView(() =>
        import("./DatabaseView").then(({ DatabaseView }) => ({ default: DatabaseView })),
      ),
    },
  ],
};
