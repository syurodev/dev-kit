import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import {
  Boxes,
  BookOpen,
  ChevronDown,
  FolderPlus,
  Settings2,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { errorMessage } from "@/bridge/errors";
import { listWorkspaces, type Workspace } from "@/bridge/workspace";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";
import {
  folderDotVar,
  workspaceColor,
  workspaceInitials,
} from "./workspaceIdentity";
import { WorkspaceDialog } from "./WorkspaceDialog";
const WorkspaceMemoryDialog = lazy(() => import("./WorkspaceMemoryDialog"));

/**
 * The trigger's identity chip: initials on a note-palette color — see
 * workspaceIdentity.ts. Sized for the trigger only; the menu row's fixed
 * 14px icon slot (components/ui/menu-row.tsx) has no room for legible
 * initials at a type-scale size, so rows use a plain colour dot instead
 * (WorkspaceDot below) — same swatch convention Notes already uses for a
 * folder's colour.
 */
function WorkspaceAvatar({ workspace }: { workspace: Workspace }) {
  return (
    <span
      aria-hidden
      className="flex size-5 shrink-0 items-center justify-center rounded-sm text-caption-11 font-semibold text-neutral-900"
      style={{ background: folderDotVar(workspaceColor(workspace.id)) }}
    >
      {workspaceInitials(workspace.name)}
    </span>
  );
}

/** An 8px colour swatch — the menu row's icon slot is too small for legible initials. */
function WorkspaceDot({ workspace }: { workspace: Workspace }) {
  return (
    <span
      aria-hidden
      className="size-2 shrink-0 rounded-full"
      style={{ background: folderDotVar(workspaceColor(workspace.id)) }}
    />
  );
}

/**
 * Title bar workspace switcher — sits right after the sidebar toggle,
 * outside the three action zones, matching the running code's real layout
 * (not the still-unimplemented redesign in title-bar.mdx). Breaks the
 * "icon + tooltip only" rule the rest of the bar follows: a workspace's name
 * is the point, and hiding it behind a tooltip defeats the switcher.
 *
 * "Current" here is UI-only (persisted in workspaceSwitcher store) — nothing
 * downstream reads it yet. Sandbox/CodeGraph wiring is workspace.mdx's
 * Registry section, still design-only.
 */
export function WorkspaceSwitcher() {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [loaded, setLoaded] = useState(false);
  // The Create / Edit dialog: null closed, "new" creating, else the one edited.
  const [dialog, setDialog] = useState<Workspace | "new" | null>(null);
  const [memoryWorkspace, setMemoryWorkspace] = useState<Workspace | null>(
    null,
  );
  const currentId = useWorkspaceSwitcherStore((s) => s.currentId);
  const setCurrent = useWorkspaceSwitcherStore((s) => s.setCurrent);
  const servicesChanged = useWorkspaceSwitcherStore((s) => s.servicesChanged);

  const refresh = useCallback(async () => {
    try {
      const list = await listWorkspaces();
      setWorkspaces(list);
      return list;
    } catch (e) {
      toast.error(errorMessage(e));
      return [];
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // No explicit selection yet (first run, or the saved one no longer exists)
  // — fall back to the first workspace so the trigger never shows a name
  // that isn't actually registered.
  useEffect(() => {
    if (!loaded || workspaces.length === 0) return;
    if (workspaces.some((w) => w.id === currentId)) return;
    setCurrent(workspaces[0].id);
  }, [loaded, workspaces, currentId, setCurrent]);

  const current =
    workspaces.find((w) => w.id === currentId) ?? workspaces[0] ?? null;

  const handleSaved = useCallback(
    (saved: Workspace) => {
      setWorkspaces((prev) =>
        prev.some((w) => w.id === saved.id)
          ? prev.map((w) => (w.id === saved.id ? saved : w))
          : [...prev, saved],
      );
      setCurrent(saved.id);
      servicesChanged();
    },
    [setCurrent, servicesChanged],
  );

  const handleRemoved = useCallback(
    (removed: Workspace) => {
      const rest = workspaces.filter((w) => w.id !== removed.id);
      setWorkspaces(rest);
      if (rest[0]) setCurrent(rest[0].id);
    },
    [workspaces, setCurrent],
  );

  return (
    <>
      <DropdownMenu onOpenChange={(open) => open && void refresh()}>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              {current ? (
                <Button
                  type="button"
                  variant="titlebar"
                  size="sm"
                  aria-label={`Workspace: ${current.name}`}
                  className="mx-1 rounded-md"
                >
                  <WorkspaceAvatar workspace={current} />
                  <span className="max-w-32 truncate text-label-12">
                    {current.name}
                  </span>
                  <ChevronDown className="size-3.5 text-text-tertiary" />
                </Button>
              ) : (
                <Button
                  type="button"
                  variant="titlebar"
                  size="icon-sm"
                  aria-label="Add workspace"
                  className="mx-1"
                >
                  <Boxes className="size-4" />
                </Button>
              )}
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            {current ? current.root : "Add workspace"}
          </TooltipContent>
        </Tooltip>

        <DropdownMenuContent align="start" className="w-72">
          {workspaces.map((ws) => (
            <DropdownMenuItem
              key={ws.id}
              onSelect={() => setCurrent(ws.id)}
              icon={<WorkspaceDot workspace={ws} />}
              detail={ws.id === current?.id ? "Current" : undefined}
            >
              {ws.name}
            </DropdownMenuItem>
          ))}
          {workspaces.length > 0 && <DropdownMenuSeparator />}
          {current ? (
            <DropdownMenuItem
              onSelect={() => setDialog(current)}
              icon={<Settings2 className="text-text-tertiary" />}
            >
              Edit workspace…
            </DropdownMenuItem>
          ) : null}
          {current && (
            <DropdownMenuItem
              onSelect={() => setMemoryWorkspace(current)}
              icon={<BookOpen className="text-text-tertiary" />}
            >
              Workspace memory…
            </DropdownMenuItem>
          )}
          <DropdownMenuItem
            onSelect={() => setDialog("new")}
            icon={<FolderPlus className="text-text-tertiary" />}
          >
            Add workspace…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {/* A sibling of the menu, not inside it: the menu closes on select and
        hands focus back to its trigger, which would fight the dialog for it. */}
      {memoryWorkspace && (
        <Suspense fallback={null}>
          <WorkspaceMemoryDialog
            key={memoryWorkspace.id}
            workspace={memoryWorkspace}
            onClose={() => setMemoryWorkspace(null)}
          />
        </Suspense>
      )}
      <WorkspaceDialog
        open={dialog !== null}
        onOpenChange={(open) => !open && setDialog(null)}
        workspace={dialog === "new" ? null : dialog}
        onSaved={handleSaved}
        onRemoved={handleRemoved}
      />
    </>
  );
}
