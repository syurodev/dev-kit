import { useState } from "react";
import { Search } from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { TOOLS, type ToolDescriptor } from "./toolCatalogue";
import { useToolsStore } from "./toolsStore";

/**
 * Figma `Tools / Sidebar Content` (1143:5): the registry as a list.
 *
 * It used to be split into "Implemented" and "Design", because six of the seven
 * were drawn and not built. They are all built now, so the split has nothing
 * left to say and the heading goes with it.
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
      <div className="pt-1.5 pb-1">
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2 size-3 -translate-y-1/2 text-text-tertiary" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search tools…"
            aria-label="Search tools"
            className="h-7 border-border-subtle bg-surface-well pl-7 text-body-12"
          />
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto scrollbar-gutter-stable">
        {matches.length === 0 ? (
          <div className="px-2 pt-6">
            <p className="text-body-12 text-text-tertiary">Nothing matches “{query.trim()}”.</p>
          </div>
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
      // h-7 rather than the h-6 this list used: a glyph beside the label needs
      // the extra pixel of breathing room, and the rail is seven rows long, so
      // it costs nothing that a scrollbar would notice.
      className={cn(
        "row-accent group flex h-7 w-full items-center gap-2 rounded-md px-2 text-left",
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
