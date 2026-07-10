import type { ModuleDefinition } from "../registry";
import { DatabaseView } from "./DatabaseView";

export const databaseModule: ModuleDefinition = {
  id: "database",
  title: "Database",
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
