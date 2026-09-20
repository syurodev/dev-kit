import { KanbanSquare } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const tasksModule: ModuleManifest = {
  id: "tasks",
  title: "Tasks",
  version: "1.0.0",
  apiVersion: "1",
  icon: KanbanSquare,
  padded: false,
  tabs: true,
  // tasks:admin is separate so an agent can hold read+write and never change
  // the workflow or the verify command that binds it.
  capabilities: ["tasks:read", "tasks:write", "tasks:admin"],
  commands: [{ id: "tasks.create", title: "New task" }],
  views: [
    {
      id: "tasks.board",
      title: "Board",
      ...lazyModuleView(() =>
        import("./TasksBoardView").then(({ TasksBoardView }) => ({ default: TasksBoardView })),
      ),
    },
    {
      id: "tasks.detail",
      title: "Task",
      ...lazyModuleView(() =>
        import("./TaskDetailView").then(({ TaskDetailView }) => ({ default: TaskDetailView })),
      ),
    },
  ],
};
