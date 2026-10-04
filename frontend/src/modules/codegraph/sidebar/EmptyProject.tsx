import { FolderGit2 } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import { SidebarSection } from "@/components/ui/sidebar-shell";
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
  const tip = "Tip: an agent can open a project too — codegraph.openProject over MCP.";
  if (services.length === 0) {
    return (
      <EmptyState
        size="rail"
        title="No project open"
        body={
          loaded
            ? "This workspace has no services yet. Add one from the workspace switcher in the title bar."
            : "Reading the workspace…"
        }
      >
        <p className="text-caption-11 text-text-tertiary">{tip}</p>
      </EmptyState>
    );
  }
  return (
    <div className="flex flex-col gap-3">
      <p className="px-3 pt-3 text-body-12 text-text-tertiary">
        No project open. Pick a service below to index its entry points, call flow and files. Nothing
        leaves this machine.
      </p>
      <div className="flex flex-col gap-0.5">
        <SidebarSection first label="Workspace services" count={services.length} />
        {services.map((svc) => (
          <button
            key={svc.id}
            type="button"
            disabled={busy}
            onClick={() => onOpenProject(svc.path)}
            className="flex w-full items-center gap-2.5 rounded-md px-3 py-1.5 text-left hover:bg-surface-raised disabled:opacity-60"
          >
            <FolderGit2 className="size-4 shrink-0 text-text-secondary" aria-hidden />
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-body-13 text-text-primary">{svc.name}</span>
              <span className="truncate text-code-11 text-text-tertiary">{svc.path}</span>
            </span>
          </button>
        ))}
      </div>
      <p className="px-3 text-caption-11 text-text-tertiary">{tip}</p>
    </div>
  );
}
