import * as React from "react";
import { Command as CommandPrimitive } from "cmdk";
import { Boxes, Check, ChevronDown, Folder, FolderX, Plus, Search, X } from "lucide-react";

import { cn } from "@/lib/utils";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

/** The last segment of a path — the folder a developer calls the project. */
function projectLabel(path: string): string {
  return path.split("/").filter(Boolean).pop() ?? path;
}

/** The folder a project sits in, home-relative, for telling two "api"s apart. */
function projectParent(path: string): string {
  const parts = path.split("/").filter(Boolean);
  parts.pop();
  const parent = `/${parts.join("/")}`;
  const home = parent.match(/^\/Users\/[^/]+|^\/home\/[^/]+/)?.[0];
  return home ? `~${parent.slice(home.length)}` : parent;
}

/**
 * Where a new session will work, picked in the draft instead of by a native
 * folder dialog before the draft opens. "+ New" goes straight to somewhere to
 * type; the folder defaults to the last one used and is changed here.
 *
 * `value` "" is no project: the session gets a scratch folder of its own when
 * it is created (see AgentHubNewScratchDirectory).
 */
export function ProjectPicker({
  value,
  projects,
  onChange,
  onChooseFolder,
  workspace = null,
  onChooseWorkspace,
  workspaceSelected = false,
  disabled = false,
}: {
  value: string;
  /** Known projects, most recent first. */
  projects: string[];
  onChange: (path: string) => void;
  /** Opens the native folder picker; the view sets the draft's folder from it. */
  onChooseFolder: () => void;
  /**
   * The current workspace (docs/dev-kit/workspace.mdx), if one is selected —
   * offered as a single item that grants every one of its services rather
   * than picking one. A workspace's services do not necessarily share a root
   * `value` could cover, so this is not the same thing as picking `paths[0]`.
   */
  workspace?: { name: string; paths: string[] } | null;
  onChooseWorkspace?: (paths: string[]) => void;
  /** True while the draft holds the whole workspace rather than one folder. */
  workspaceSelected?: boolean;
  disabled?: boolean;
}) {
  const [open, setOpen] = React.useState(false);
  const [query, setQuery] = React.useState("");
  const pick = (action: () => void) => {
    setOpen(false);
    setQuery("");
    action();
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setQuery("");
      }}
    >
      <PopoverTrigger asChild disabled={disabled}>
        <button
          type="button"
          data-testid="draft-project-picker"
          title={value || undefined}
          className={cn(
            "flex h-(--control-height-sm) min-w-0 max-w-60 items-center gap-1.5 rounded-md px-2 text-body-13 text-text-secondary outline-none",
            "hover:bg-state-soft hover:text-text-primary focus-visible:ring-1 focus-visible:ring-status-info",
            "disabled:pointer-events-none disabled:opacity-60 data-[state=open]:bg-state-soft",
          )}
        >
          {workspaceSelected ? (
            <Boxes aria-hidden="true" className="size-3.5 shrink-0" />
          ) : value ? (
            <Folder aria-hidden="true" className="size-3.5 shrink-0" />
          ) : (
            <FolderX aria-hidden="true" className="size-3.5 shrink-0" />
          )}
          <span className="min-w-0 truncate">
            {workspaceSelected && workspace
              ? `${workspace.name} (${workspace.paths.length})`
              : value
                ? projectLabel(value)
                : "No project"}
          </span>
          <ChevronDown aria-hidden="true" className="size-3 shrink-0 text-text-tertiary" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" side="top" className="w-80 gap-0 p-1.5">
        <CommandPrimitive
          data-testid="draft-project-menu"
          // Filters on the label and the path both: "billing" and "acme/" each
          // find the project.
          filter={(itemValue, search) => (itemValue.toLowerCase().includes(search.toLowerCase()) ? 1 : 0)}
          className="flex flex-col"
        >
          <div className="flex h-9 items-center gap-2 px-2">
            <Search aria-hidden="true" className="size-3.5 shrink-0 text-text-tertiary" />
            <CommandPrimitive.Input
              value={query}
              onValueChange={setQuery}
              placeholder="Search projects"
              className="min-w-0 flex-1 bg-transparent text-body-13 text-text-primary outline-none placeholder:text-text-tertiary"
            />
          </div>
          <CommandPrimitive.List className="max-h-64 overflow-y-auto">
            <CommandPrimitive.Empty className="px-2 py-3 text-body-13 text-text-tertiary">
              No matching project
            </CommandPrimitive.Empty>
            {/* Above the individual services, on purpose: it is the more
                deliberate choice of the two, and a workspace with many
                services would otherwise bury it under them. */}
            {workspace && onChooseWorkspace ? (
              <CommandPrimitive.Item
                value={`${workspace.name} workspace all folders`}
                data-testid="draft-project-workspace"
                onSelect={() => pick(() => onChooseWorkspace(workspace.paths))}
                className={itemClass}
              >
                <Boxes aria-hidden="true" className="size-3.5 shrink-0 text-icon-secondary" />
                <span className="shrink-0 text-text-primary">{workspace.name}</span>
                <span className="min-w-0 flex-1 truncate text-text-tertiary">
                  all {workspace.paths.length} folders
                </span>
                {workspaceSelected ? <Check aria-hidden="true" className="size-3.5 shrink-0" /> : null}
              </CommandPrimitive.Item>
            ) : null}
            {projects.map((path) => (
              <CommandPrimitive.Item
                key={path}
                value={`${projectLabel(path)} ${path}`}
                data-testid="draft-project-item"
                onSelect={() => pick(() => onChange(path))}
                className={itemClass}
              >
                <Folder aria-hidden="true" className="size-3.5 shrink-0 text-icon-secondary" />
                <span className="shrink-0 text-text-primary">{projectLabel(path)}</span>
                <span className="min-w-0 flex-1 truncate text-text-tertiary">{projectParent(path)}</span>
                {!workspaceSelected && path === value ? <Check aria-hidden="true" className="size-3.5 shrink-0" /> : null}
              </CommandPrimitive.Item>
            ))}
          </CommandPrimitive.List>
          <div className="my-1 h-px bg-border-subtle" />
          {/* Outside the list, so a search never hides them: a project that is
              not in the list yet is exactly when "New project" is wanted. */}
          <CommandPrimitive.Item
            value="__new"
            forceMount
            data-testid="draft-project-new"
            onSelect={() => pick(onChooseFolder)}
            className={itemClass}
          >
            <Plus aria-hidden="true" className="size-3.5 shrink-0" />
            <span className="flex-1">New project…</span>
          </CommandPrimitive.Item>
          <CommandPrimitive.Item
            value="__none"
            forceMount
            data-testid="draft-project-none"
            onSelect={() => pick(() => onChange(""))}
            className={itemClass}
          >
            <X aria-hidden="true" className="size-3.5 shrink-0" />
            <span className="flex-1">Don&apos;t work in a project</span>
            {value === "" ? <Check aria-hidden="true" className="size-3.5 shrink-0" /> : null}
          </CommandPrimitive.Item>
        </CommandPrimitive>
      </PopoverContent>
    </Popover>
  );
}

const itemClass = cn(
  "flex h-8 cursor-pointer items-center gap-2 rounded-md px-2 text-body-13 text-text-secondary outline-none select-none",
  "data-[selected=true]:bg-state-soft data-[selected=true]:text-text-primary",
);
