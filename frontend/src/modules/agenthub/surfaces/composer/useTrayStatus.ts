import { useEffect, useState } from "react";

import { gitRepositories } from "@/bridge/git";
import { listBoards, listTasks, onTasksChanged, type Board, type Task } from "@/bridge/tasks";
import { listWorkspaceServices, listWorkspaces } from "@/bridge/workspace";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";

export type TrayWorkspace = { id: string; name: string; folders: number };

/** A task the strip counts, with what the list beside it needs to say. */
export type TrayTask = { task: Task; board: Board; statusName: string; active: boolean; mine: boolean };

export type TrayStatus = {
  /** The workspace whose folders include this one, if any does. */
  workspace: TrayWorkspace | null;
  /** The branch the folder's repository is on; null outside a repository. */
  branch: string | null;
  /** Whether any board is bound to the workspace — without one there is no task count to give. */
  hasBoards: boolean;
  tasks: TrayTask[];
};

const EMPTY: TrayStatus = { workspace: null, branch: null, hasBoards: false, tasks: [] };

function within(path: string, folder: string): boolean {
  const root = folder.replace(/[\\/]+$/, "");
  return path === root || path.startsWith(root + "/") || path.startsWith(root + "\\");
}

/**
 * The workspace a folder belongs to. A session does not record one — it
 * records its folder — so it is found by asking which registered workspace
 * lists that folder, the current workspace first since that is nearly always
 * the answer.
 */
async function findWorkspace(projectPath: string, currentId: string | null): Promise<TrayWorkspace | null> {
  const workspaces = await listWorkspaces();
  const ordered = [...workspaces].sort((a, b) => Number(b.id === currentId) - Number(a.id === currentId));
  for (const workspace of ordered) {
    const services = await listWorkspaceServices(workspace.id);
    if (services.some((service) => within(projectPath, service.path))) {
      return { id: workspace.id, name: workspace.name, folders: services.length };
    }
  }
  return null;
}

async function findBranch(projectPath: string): Promise<string | null> {
  const repos = await gitRepositories(projectPath);
  const repo = repos.find((r) => r.path === projectPath) ?? (repos.length === 1 ? repos[0] : undefined);
  if (!repo) return null;
  return repo.detached ? "detached" : repo.branch || null;
}

/**
 * The tasks worth counting for this work: every task still open or in
 * progress on a board bound to the workspace, those this session holds first.
 * A board's workflow names its statuses, so every rule reads a status's
 * category, never its name (a terminal column may be called anything).
 */
async function findTasks(workspaceId: string, sessionId: string): Promise<{ hasBoards: boolean; tasks: TrayTask[] }> {
  const boards = (await listBoards()).filter((board) => board.workspaceId === workspaceId);
  const lists = await Promise.all(boards.map((board) => listTasks(board.id).then((tasks) => ({ board, tasks }))));
  const out: TrayTask[] = [];
  for (const { board, tasks } of lists) {
    for (const task of tasks) {
      const status = board.workflow.statuses.find((s) => s.id === task.status);
      if (task.archivedAt || !status || status.category === "terminal") continue;
      out.push({
        task,
        board,
        statusName: status.name,
        active: status.category === "active",
        mine: Boolean(sessionId) && task.sessionId === sessionId,
      });
    }
  }
  out.sort((a, b) => Number(b.mine) - Number(a.mine) || Number(b.active) - Number(a.active));
  return { hasBoards: boards.length > 0, tasks: out };
}

/**
 * What the composer tray's strip says about where this work is: workspace,
 * branch and tasks. Each is read on its own, so a folder outside any git
 * repository or workspace still gets the rest, and a failed read leaves only
 * its own part empty.
 *
 * The branch is read again whenever `refreshKey` changes — the caller passes
 * something that moves when a turn ends, since that is when an agent may have
 * switched it. Tasks follow tasks:changed.
 */
export function useTrayStatus(projectPath: string, sessionId: string, refreshKey: unknown): TrayStatus {
  const currentId = useWorkspaceSwitcherStore((s) => s.currentId);
  const servicesVersion = useWorkspaceSwitcherStore((s) => s.servicesVersion);
  const [workspace, setWorkspace] = useState<TrayWorkspace | null>(null);
  const [branch, setBranch] = useState<string | null>(null);
  const [tasks, setTasks] = useState<{ hasBoards: boolean; tasks: TrayTask[] }>({ hasBoards: false, tasks: [] });
  const [tasksVersion, setTasksVersion] = useState(0);

  useEffect(() => {
    setWorkspace(null);
    if (!projectPath) return;
    let live = true;
    findWorkspace(projectPath, currentId)
      .then((found) => live && setWorkspace(found))
      .catch(() => live && setWorkspace(null));
    return () => {
      live = false;
    };
  }, [projectPath, currentId, servicesVersion]);

  useEffect(() => {
    if (!projectPath) {
      setBranch(null);
      return;
    }
    let live = true;
    findBranch(projectPath)
      .then((found) => live && setBranch(found))
      .catch(() => live && setBranch(null));
    return () => {
      live = false;
    };
  }, [projectPath, refreshKey]);

  const workspaceId = workspace?.id ?? "";
  useEffect(() => {
    if (!workspaceId) return;
    // Any board's change re-reads: which boards are bound can change too.
    return onTasksChanged(() => setTasksVersion((v) => v + 1));
  }, [workspaceId]);

  useEffect(() => {
    if (!workspaceId) {
      setTasks({ hasBoards: false, tasks: [] });
      return;
    }
    let live = true;
    const timer = setTimeout(() => {
      findTasks(workspaceId, sessionId)
        .then((found) => live && setTasks(found))
        .catch(() => live && setTasks({ hasBoards: false, tasks: [] }));
    }, tasksVersion ? 150 : 0);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [workspaceId, sessionId, tasksVersion]);

  if (!projectPath) return EMPTY;
  return { workspace, branch, hasBoards: tasks.hasBoards, tasks: tasks.tasks };
}
