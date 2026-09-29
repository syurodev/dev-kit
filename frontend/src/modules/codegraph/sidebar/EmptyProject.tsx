import { FolderGit2 } from "lucide-react";
import { useCurrentWorkspaceServices } from "@/stores/workspaceSwitcher";

/**
 * Figma `CodeGraph / Sidebar — 2 No project` (State=Empty), adapted: the way
 * in is the current workspace's services, not a free-standing folder picker
 * — adding a folder is the title bar workspace switcher's job now.
 */
export function EmptyProject({
  busy,
  onOpenProject,
}: {
  busy: boolean;
  onOpenProject: (path: string) => void;
}) {
  const { services, loaded } = useCurrentWorkspaceServices();
  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1.5 px-1 pt-2">
        <h2 className="text-title-14 text-text-primary">No project open</h2>
        <p className="text-body-12 text-text-tertiary">
          {services.length > 0
            ? "Pick a service below to index its entry points, call flow and files. Nothing leaves this machine."
            : loaded
              ? "This workspace has no services yet. Add one from the workspace switcher in the title bar."
              : "Reading the workspace…"}
        </p>
      </div>
      {services.length > 0 ? (
        <div className="flex flex-col gap-0.5">
          <span className="px-1 pb-1.5 text-label-11-caps text-text-tertiary">Workspace services</span>
          {services.map((svc) => (
            <button
              key={svc.id}
              type="button"
              disabled={busy}
              onClick={() => onOpenProject(svc.path)}
              className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left hover:bg-state-soft disabled:opacity-60"
            >
              <FolderGit2 className="size-4 shrink-0 text-text-secondary" aria-hidden />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate text-body-13 text-text-primary">{svc.name}</span>
                <span className="truncate text-code-11 text-text-tertiary">{svc.path}</span>
              </span>
            </button>
          ))}
        </div>
      ) : null}
      <p className="px-1 text-caption-11 text-text-tertiary">Tip: an agent can open a project too — codegraph.openProject over MCP.</p>
    </div>
  );
}
