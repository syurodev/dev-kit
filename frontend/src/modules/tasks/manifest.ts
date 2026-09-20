import { KanbanSquare } from "lucide-react";
import { PLAN_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { registerIntent } from "@/workbench/intents";

export const tasksModule: ModuleManifest = {
  id: "tasks",
  title: "Tasks",
  version: "1.0.0",
  apiVersion: "1",
  icon: KanbanSquare,
  group: PLAN_GROUP,
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

/**
 * `tasks.reveal` (docs/dev-kit/cross-module.mdx): open a task from anywhere —
 * a chip on Code Graph's plan canvas, a link in a commit — without the caller
 * importing this module's views or its store.
 *
 * Registered with the manifest, which is eager; the document helper loads on
 * the first dispatch. `beside` by default: a chip is clicked while reading the
 * plan, and replacing the canvas would take away the thing being read.
 */
registerIntent(
  "tasks.reveal",
  async (ref, { placement }) => {
    if (ref.kind !== "task") return;
    const { openTaskDetail } = await import("./documents");
    openTaskDetail(ref.id, ref.label, { preview: false, beside: placement === "beside" });
  },
  "beside",
);
