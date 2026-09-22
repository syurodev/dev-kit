import { useCallback, useEffect, useState } from "react";
import { Boxes, ChevronDown, FolderPlus } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { errorMessage } from "@/bridge/errors";
import {
  addWorkspace,
  chooseWorkspaceFolder,
  listWorkspaces,
  type Workspace,
} from "@/bridge/workspace";
import { useWorkspaceSwitcherStore } from "@/stores/workspaceSwitcher";
import { folderDotVar, workspaceColor, workspaceInitials } from "./workspaceIdentity";

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
  const [adding, setAdding] = useState(false);
  const currentId = useWorkspaceSwitcherStore((s) => s.currentId);
  const setCurrent = useWorkspaceSwitcherStore((s) => s.setCurrent);

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

  const current = workspaces.find((w) => w.id === currentId) ?? workspaces[0] ?? null;

  const handleAdd = useCallback(async () => {
    setAdding(true);
    try {
      const path = await chooseWorkspaceFolder(current?.root ?? "");
      if (!path) return; // cancelled
      const result = await addWorkspace("", path);
      setWorkspaces((prev) => [...prev, result.workspace]);
      setCurrent(result.workspace.id);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setAdding(false);
    }
  }, [current?.root, setCurrent]);

  return (
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
                <span className="max-w-32 truncate text-label-12">{current.name}</span>
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
        <DropdownMenuItem
          disabled={adding}
          onSelect={(event) => {
            event.preventDefault(); // stay open through the native folder dialog
            void handleAdd();
          }}
          icon={<FolderPlus className="text-text-tertiary" />}
        >
          Add workspace…
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
