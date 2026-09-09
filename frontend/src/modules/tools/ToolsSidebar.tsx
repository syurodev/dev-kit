import { useState } from "react";
import { Search } from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { useToolsStore } from "./toolsStore";

/** internal/commands.Registry has exactly one handler. */
const IMPLEMENTED = [{ id: "tools.hash", name: "Hash" }];
/** tools.mdx § "Design: thêm util command" — designed, not registered. */
const DESIGNED = [
  { id: "tools.base64Encode", name: "Base64 encode" },
  { id: "tools.base64Decode", name: "Base64 decode" },
  { id: "tools.uuidGenerate", name: "UUID generate" },
  { id: "tools.urlEncode", name: "URL encode" },
  { id: "tools.urlDecode", name: "URL decode" },
  { id: "tools.jsonFormat", name: "JSON format" },
];

/**
 * Figma `Tools / Sidebar Content` (1143:5): the registry as a list, split by
 * what actually exists. A flat list of seven would say nothing about what
 * you can run today; the DESIGN group is drawn but not clickable.
 */
export function ToolsSidebar() {
  const selected = useToolsStore((s) => s.selected);
  const select = useToolsStore((s) => s.select);
  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const show = (t: { id: string; name: string }) =>
    !needle || t.name.toLowerCase().includes(needle) || t.id.toLowerCase().includes(needle);

  const Row = ({ id, name, enabled }: { id: string; name: string; enabled: boolean }) => (
    <button
      type="button"
      disabled={!enabled}
      onClick={() => select(id)}
      title={enabled ? undefined : "Designed in tools.mdx — not built yet"}
      className={cn(
        "flex h-6 w-full items-center gap-1.5 rounded-md px-2 text-left",
        selected === id
          ? "bg-surface-raised text-text-primary"
          : "text-text-secondary hover:bg-surface-raised/60 hover:text-text-primary",
        !enabled && "opacity-45 hover:bg-transparent hover:text-text-secondary",
      )}
    >
      <span className="min-w-0 truncate text-body-13">{name}</span>
      {/* The id rides behind the name: it is what the palette, the MCP tool
          and the capability grant all key on. */}
      <span className="ml-auto shrink-0 text-code-11 text-text-tertiary">{id}</span>
    </button>
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
      <div className="min-h-0 flex-1 overflow-y-auto">
        <span className="flex h-8 items-center px-2 text-label-11-caps text-text-tertiary">Implemented</span>
        {IMPLEMENTED.filter(show).map((t) => <Row key={t.id} {...t} enabled />)}
        <span className="flex h-8 items-center px-2 text-label-11-caps text-text-tertiary">Design</span>
        {DESIGNED.filter(show).map((t) => <Row key={t.id} {...t} enabled={false} />)}
      </div>
      <div className="flex h-10 shrink-0 items-center px-2 text-code-11 text-text-tertiary">
        pure functions · nothing is stored
      </div>
    </div>
  );
}
