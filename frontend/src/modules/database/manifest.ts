import type { ModuleManifest } from "../registry";
import { DatabaseView } from "./DatabaseView";

export const databaseModule: ModuleManifest = {
  id: "database",
  title: "Database",
  version: "1.0.0",
  apiVersion: "1",
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
      component: DatabaseView,
    },
  ],
};
