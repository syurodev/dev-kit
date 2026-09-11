import { useState } from "react";
import {
  SIDEBAR_ROW_HEIGHT,
  SidebarHeader,
  SidebarSearch,
} from "@/components/ui/sidebar-shell";
import { Empty, EmptyDescription } from "@/components/ui/empty";
import { cn } from "@/lib/utils";
import { TOOLS, type ToolDescriptor } from "./toolCatalogue";
import { useToolsStore } from "./toolsStore";

/**
 * Figma `Tools / Sidebar Content` (1143:5): the registry as a list.
 *
 * Flat, with no headings: every tool is built, so there is nothing for a
 * grouping to say.
 */
export function ToolsSidebar() {
  const selected = useToolsStore((s) => s.selected);
  const select = useToolsStore((s) => s.select);
  const [query, setQuery] = useState("");

  const needle = query.trim().toLowerCase();
  const matches = TOOLS.filter(
    (tool) =>
      !needle ||
      tool.name.toLowerCase().includes(needle) ||
      tool.id.toLowerCase().includes(needle),
  );

  return (
    <div className="flex h-full flex-col select-none">
      <SidebarHeader>
        <SidebarSearch
          value={query}
          onChange={setQuery}
          placeholder="Search tools…"
          aria-label="Search tools"
        />
      </SidebarHeader>

      <div className="min-h-0 flex-1 overflow-y-auto scrollbar-gutter-stable">
        {matches.length === 0 ? (
          <Empty className="px-2 pt-6">
            <EmptyDescription>Nothing matches “{query.trim()}”.</EmptyDescription>
          </Empty>
        ) : (
          matches.map((tool) => (
            <ToolRow
              key={tool.id}
              tool={tool}
              active={selected === tool.id}
              onSelect={() => select(tool.id)}
            />
          ))
        )}
      </div>

      <div className="flex h-10 shrink-0 items-center px-2 text-code-11 text-text-tertiary">
        pure functions · nothing is stored
      </div>
    </div>
  );
}

function ToolRow({
  tool,
  active,
  onSelect,
}: {
  tool: ToolDescriptor;
  active: boolean;
  onSelect: () => void;
}) {
  const Icon = tool.icon;
  return (
    <button
      type="button"
      onClick={onSelect}
      data-accent={active ? "on" : undefined}
      // A flat list of eight, so the `list` tier: nothing here is deep enough
      // to want the tree tier's density.
      className={cn(
        "row-accent group flex w-full items-center gap-2 rounded-md px-3 text-left",
        SIDEBAR_ROW_HEIGHT.list,
        active
          ? "bg-surface-raised text-text-primary"
          : "text-text-secondary hover:bg-surface-raised/60 hover:text-text-primary",
      )}
    >
      <Icon
        className={cn(
          "size-(--icon-14) shrink-0",
          active ? "text-text-primary" : "text-text-tertiary group-hover:text-text-secondary",
        )}
      />
      <span className="min-w-0 truncate text-body-13">{tool.name}</span>
      {/* The id rides behind the name: it is what the palette, the MCP tool
          and the capability grant all key on. */}
      <span className="ml-auto shrink-0 text-code-11 text-text-tertiary">{tool.id}</span>
    </button>
  );
}
