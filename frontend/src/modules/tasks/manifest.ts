import { lazy, Suspense, createElement } from "react";
import { KanbanSquare } from "lucide-react";
import { PLAN_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { registerIntent } from "@/workbench/intents";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { parseTaskResult } from "./taskResult";

export const tasksModule: ModuleManifest = {
  id: "tasks",
  title: "Tasks",
  // The board is what anyone arriving at Tasks is looking for. Its home costs nothing to open, so it opens
  // rather than showing "has nothing open" (ModuleManifest.opensHome).
  opensHome: true,
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

/**
 * ToolResultView for the four task tools that answer with a full Task
 * (docs/dev-kit/tool-result-view.mdx) — one card component, one registration
 * per tool name since the registry keys on the tool, not the shape.
 */
const LazyTaskResultCard = lazy(() =>
  import("./TaskResultCard").then(({ TaskResultCard }) => ({ default: TaskResultCard })),
);

function registerTaskResultView(toolName: string): void {
  registerToolResultView(toolName, ({ rawOutput }) => {
    const task = parseTaskResult(rawOutput);
    if (!task) return null;
    return createElement(Suspense, { fallback: null }, createElement(LazyTaskResultCard, { task }));
  });
}

for (const toolName of ["tasks.create", "tasks.get", "tasks.update", "tasks.transition"]) {
  registerTaskResultView(toolName);
}
