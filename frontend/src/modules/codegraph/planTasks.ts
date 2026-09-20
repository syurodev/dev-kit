import type { PlanTaskChip } from "./codegraphStore";
import type { Board, Task } from "@/bridge/tasks";

/**
 * Which board, if any, covers the project Code Graph has open.
 *
 * A board declares the project it belongs to. One that declares nothing, or
 * declares a different project, is somebody else's board — drawing its tasks
 * on this plan would be answering a question nobody asked. Most projects have
 * no board at all, and that is the quiet, common answer.
 */
export function boardForProject(boards: Board[], projectRoot: string): Board | null {
  if (!projectRoot) return null;
  return boards.find((board) => board.projectPath === projectRoot) ?? null;
}

/**
 * Narrows the tasks to what a chip draws, and drops the ones a chip should not
 * carry: an archived task is not current work, and showing it would make a
 * plan node look busier than it is.
 */
export function chipsByPlanNode(byNode: Record<string, Task[]>): Record<string, PlanTaskChip[]> {
  const chips: Record<string, PlanTaskChip[]> = {};
  for (const [planNodeId, tasks] of Object.entries(byNode)) {
    const live = tasks
      .filter((task) => !task.archivedAt)
      .map((task) => ({ id: task.id, key: task.key, title: task.title, status: task.status }));
    if (live.length > 0) {
      chips[planNodeId] = live;
    }
  }
  return chips;
}
