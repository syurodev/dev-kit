import { FolderGit2, FolderOpen } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useRecentProjectsStore } from "../recentProjects";
import { useNow } from "./ProjectHeader";
import { projectLabel, relativeTime } from "./sidebarModel";

/** Figma `CodeGraph / Sidebar — 2 No project` (State=Empty). */
export function EmptyProject({
  busy,
  onPickProject,
  onOpenProject,
}: {
  busy: boolean;
  onPickProject: () => void;
  onOpenProject: (path: string) => void;
}) {
  const recent = useRecentProjectsStore((s) => s.projects);
  const now = useNow();
  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1.5 px-1 pt-2">
        <h2 className="text-title-14 text-text-primary">No project open</h2>
        <p className="text-body-12 text-text-tertiary">
          Open a Java, Lua or TypeScript project to index its endpoints, call flow and files. Nothing leaves this
          machine.
        </p>
      </div>
      <Button onClick={onPickProject} disabled={busy} className="w-full">
        <FolderOpen />
        Open project…
      </Button>
      {recent.length > 0 ? (
        <div className="flex flex-col gap-0.5">
          <span className="px-1 pb-1.5 text-label-11-caps text-text-tertiary">Recent</span>
          {recent.map((p) => {
            const { name, where } = projectLabel(p.path);
            return (
              <button
                key={p.path}
                type="button"
                disabled={busy}
                onClick={() => onOpenProject(p.path)}
                className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left hover:bg-state-soft disabled:opacity-60"
              >
                <FolderGit2 className="size-4 shrink-0 text-text-secondary" aria-hidden />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate text-body-13 text-text-primary">{name}</span>
                  <span className="truncate text-code-11 text-text-tertiary">{where}</span>
                </span>
                <span className="shrink-0 text-caption-11 text-text-tertiary">{relativeTime(p.openedAt, now)}</span>
              </button>
            );
          })}
        </div>
      ) : null}
      <p className="px-1 text-caption-11 text-text-tertiary">Tip: an agent can open a project too — codegraph.openProject over MCP.</p>
    </div>
  );
}
