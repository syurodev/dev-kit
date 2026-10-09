import type { AgentSession } from "@/bridge/agenthub";

/**
 * A workspace as the rail groups by it: its services' folders, primary first.
 * A session opened in any of them belongs to the workspace, not to the one
 * folder its process happens to start in.
 */
export type SidebarWorkspace = { id: string; name: string; paths: string[] };

/**
 * The last segment of a path — the folder a developer calls the project — or
 * the last `segments` of them, to tell two folders of one name apart.
 */
export function projectLabel(path: string, segments = 1): string {
  const parts = path.split("/").filter(Boolean);
  return parts.length ? parts.slice(-segments).join("/") : path;
}

/**
 * How many sessions go with each one if it is deleted, itself excluded.
 *
 * This number is the label on a destructive action, and a wrong one means the
 * user agreed to lose something they were not told about.
 */
export function countDescendants(sessions: AgentSession[]): Record<string, number> {
  const children = new Map<string, string[]>();
  for (const session of sessions) {
    if (!session.parentSessionId) continue;
    const list = children.get(session.parentSessionId) ?? [];
    list.push(session.id);
    children.set(session.parentSessionId, list);
  }
  const counts: Record<string, number> = {};
  const count = (id: string): number => {
    if (counts[id] !== undefined) return counts[id];
    counts[id] = 0; // guards a cycle, which the depth limit should prevent anyway
    let total = 0;
    for (const child of children.get(id) ?? []) total += 1 + count(child);
    counts[id] = total;
    return total;
  };
  for (const session of sessions) count(session.id);
  return counts;
}

/**
 * Where a session is filed: the one rule both the tree and the card list obey,
 * so the two can never disagree about membership.
 *
 *   scratch   every folder under the scratch root is one "no project"
 *   workspace the session's stored workspace id — a folder shared by several
 *             workspaces belongs only to the one the session chose
 *   folder    the session's own project path
 *
 * `key` is the group's identity. Never a display name or a basename: two
 * folders can share one.
 */
export function sessionGroup(
  session: Pick<AgentSession, "projectPath" | "workspaceId">,
  scratchRoot: string,
  workspaces: SidebarWorkspace[],
): { key: string; workspace?: SidebarWorkspace } {
  const path = session.projectPath;
  if (scratchRoot && path.startsWith(`${scratchRoot}/`)) return { key: scratchRoot };
  const workspace = workspaces.find((w) => w.id === session.workspaceId);
  if (!workspace) return { key: path };
  return { key: `workspace:${workspace.id}`, workspace };
}
