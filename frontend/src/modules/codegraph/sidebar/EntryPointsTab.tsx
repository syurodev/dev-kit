import { SidebarInset } from "@/components/ui/sidebar-shell";
import { useMemo } from "react";
import {
  Bot,
  Boxes,
  Braces,
  Cable,
  Clock,
  CodeXml,
  Columns2,
  Copy,
  Ellipsis,
  Globe,
  MessageSquare,
  Network,
  Puzzle,
  Send,
  SquarePlus,
  Workflow,
  type LucideIcon,
} from "lucide-react";
import { TreeRowAction, TreeView, type TreeNodeData } from "@/components/tree-view";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { dispatchIntent, hasIntent } from "@/workbench/intents";
import type { CodeGraphEntryPoint } from "@/bridge/codegraph";
import { verbChipClass } from "../graph/tokens";
import { filterEntryPoints, groupEntryPoints, verbsIn, type KindGroup, type OwnerGroup } from "./sidebarModel";

/** Figma `CodeGraph / Sidebar / Method Chip` (1189:41359): Geist Mono 10, radius 6. */
export const VERB_CHIP = "inline-flex h-5.25 shrink-0 items-center justify-center rounded-sm px-1.5 text-code-11 font-semibold uppercase";

/** The filter strip's chips: ALL, and one per verb present. */
const FILTER_CHIP = "inline-flex h-5.5 shrink-0 items-center rounded-sm border px-2 text-code-11";

/** What each kind's heading and its owners' rows are marked with. */
const KIND_ICON: Record<string, LucideIcon> = {
  http: Globe,
  graphql: Braces,
  rpc: Network,
  message: MessageSquare,
  socket: Cable,
  schedule: Clock,
  plugin: Puzzle,
};
const kindIcon = (kind: string) => KIND_ICON[kind] ?? Workflow;

type Props = {
  entryPoints: CodeGraphEntryPoint[];
  query: string;
  busy: boolean;
  selectedEntryPointId: string | null;
  verbFilter: string[];
  collapsed: string[];
  onVerbFilter: (verbs: string[]) => void;
  onToggleGroup: (key: string) => void;
  /** Single click: the flow in the preview tab. */
  onSelect: (ep: CodeGraphEntryPoint) => void;
  /** Double click: the flow in a tab that stays. */
  onKeep: (ep: CodeGraphEntryPoint) => void;
  /** The flow in a group beside the one in front. */
  onOpenBeside: (ep: CodeGraphEntryPoint) => void;
  onAskAgent: (ep: CodeGraphEntryPoint) => void;
};

/**
 * The project's entry points — HTTP routes, Kong plugin phases, message
 * handlers, scheduled jobs — grouped by kind, then service, then what declares
 * them (Figma `CodeGraph / Sidebar — 1 Endpoints grouped`, 1189:65405). A level
 * with a single group is skipped: one heading over everything says nothing.
 */
export function EntryPointsTab({
  entryPoints,
  query,
  busy,
  selectedEntryPointId,
  verbFilter,
  collapsed,
  onVerbFilter,
  onToggleGroup,
  onSelect,
  onKeep,
  onOpenBeside,
  onAskAgent,
}: Props) {
  const verbs = useMemo(() => verbsIn(entryPoints), [entryPoints]);
  const active = useMemo(() => new Set(verbFilter), [verbFilter]);
  const shown = useMemo(() => filterEntryPoints(entryPoints, query, active), [entryPoints, query, active]);
  const groups = useMemo(() => groupEntryPoints(shown), [shown]);
  // A search opens everything: a match inside a folded group is not a match.
  const searching = query.trim().length > 0 || active.size > 0;

  const menuActions = useMemo(() => ({ onKeep, onOpenBeside, onAskAgent }), [onKeep, onOpenBeside, onAskAgent]);
  const nodes = useMemo(() => {
    const closed = new Set(searching ? [] : collapsed);
    const entryNode = (ep: CodeGraphEntryPoint, owner: string): TreeNodeData => ({
      id: ep.id,
      label: (
        <>
          {/* min-w, not w: a Kong phase (BODY_FILTER, HEADER_FILTER) is
              longer than an HTTP method and needs to grow past the 48px an
              HTTP method chip sizes to, or its own text overflows the box
              uncontained (no overflow-hidden on the chip) and bleeds into the
              row's truncate boundary and the label beside it. */}
          <span className={cn(VERB_CHIP, "mr-2 min-w-12 align-middle", verbChipClass(ep.verb))}>{ep.verb}</span>
          {/* Under its plugin, a phase's target is the plugin again: the
              chip alone says which phase it is. */}
          {ep.kind === "plugin" && ep.target === ep.group ? null : ep.target}
        </>
      ),
      title: `${ep.verb} ${ep.target}`,
      selected: selectedEntryPointId === ep.id,
      onSelect: () => onSelect(ep),
      onActivate: () => onKeep(ep),
      actions: (
        <>
          <Tooltip>
            <TooltipTrigger asChild>
              <TreeRowAction aria-label={`Ask agent about ${ep.verb} ${ep.target}`} onClick={() => onAskAgent(ep)}>
                <Bot />
              </TreeRowAction>
            </TooltipTrigger>
            <TooltipContent>Ask agent</TooltipContent>
          </Tooltip>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <TreeRowAction aria-label={`More for ${ep.verb} ${ep.target}`}>
                <Ellipsis />
              </TreeRowAction>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-60">
              <EntryPointMenuItems entry={ep} owner={owner} actions={menuActions} Item={DropdownMenuItem} />
            </DropdownMenuContent>
          </DropdownMenu>
        </>
      ),
      wrap: (row) => (
        <ContextMenu key={`ep:${ep.id}`}>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <ContextMenuContent className="w-60">
            <EntryPointMenuItems entry={ep} owner={owner} actions={menuActions} Item={ContextMenuItem} />
          </ContextMenuContent>
        </ContextMenu>
      ),
    });
    const group = (key: string, label: string, title: string | undefined, Icon: LucideIcon, count: number, sticky: number, children: TreeNodeData[]): TreeNodeData => ({
      id: key,
      label,
      title,
      icon: <Icon />,
      count,
      sticky,
      expandable: true,
      expanded: !closed.has(key),
      onToggle: () => onToggleGroup(key),
      children,
    });
    const ownerNode = (o: OwnerGroup, kind: string, sticky: number) =>
      group(o.key, o.label, o.owner, kind === "plugin" ? Puzzle : CodeXml, o.entries.length, sticky, o.entries.map((ep) => entryNode(ep, o.owner)));
    const serviceNodes = (k: KindGroup, sticky: number): TreeNodeData[] =>
      k.services.length === 1
        ? k.services[0].owners.map((o) => ownerNode(o, k.kind, sticky))
        : k.services.map((svc) =>
            group(svc.key, svc.label || "default", undefined, Boxes, svc.count, sticky, svc.owners.map((o) => ownerNode(o, k.kind, sticky + 1))),
          );
    if (groups.length === 1) return serviceNodes(groups[0], 0);
    return groups.map((k) => group(k.key, k.label, undefined, kindIcon(k.kind), k.count, 0, serviceNodes(k, 1)));
  }, [groups, searching, collapsed, selectedEntryPointId, onSelect, onKeep, onAskAgent, onToggleGroup, menuActions]);

  const toggleVerb = (v: string) => onVerbFilter(active.has(v) ? verbFilter.filter((x) => x !== v) : [...verbFilter, v]);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      {verbs.length > 1 ? (
        <SidebarInset className="flex flex-wrap items-center gap-1" role="group" aria-label="Filter by verb">
          <button
            type="button"
            aria-pressed={active.size === 0}
            onClick={() => onVerbFilter([])}
            className={cn(
              FILTER_CHIP,
              active.size === 0
                ? "border-(--border-default) bg-surface-raised text-text-primary"
                : "border-border-subtle text-text-tertiary hover:text-text-secondary",
            )}
          >
            ALL
          </button>
          {verbs.map((v) => (
            <button
              key={v}
              type="button"
              aria-pressed={active.has(v)}
              onClick={() => toggleVerb(v)}
              className={cn(
                VERB_CHIP,
                verbChipClass(v),
                // Off chips recede rather than vanish: the strip keeps its shape.
                active.size > 0 && !active.has(v) && "opacity-40 hover:opacity-70",
                active.size === 0 && "opacity-70 hover:opacity-100",
              )}
            >
              {v}
            </button>
          ))}
        </SidebarInset>
      ) : null}

      {nodes.length > 0 ? (
        <TreeView nodes={nodes} className="min-h-0 flex-1 scrollbar-gutter-stable" />
      ) : !busy ? (
        <p className="px-2 py-3 text-center text-body-12 text-text-tertiary">
          {entryPoints.length === 0 ? "No entry points found in this project." : "No entry points match."}
        </p>
      ) : null}
    </div>
  );
}

type MenuActions = Pick<Props, "onKeep" | "onOpenBeside" | "onAskAgent">;

/**
 * What can be done with an entry point without picking it first — on hover,
 * and the same list on right-click. Only an HTTP route is a request the API
 * Client can send.
 */
function EntryPointMenuItems({
  entry,
  owner,
  actions,
  Item,
}: {
  entry: CodeGraphEntryPoint;
  owner: string;
  actions: MenuActions;
  Item: typeof DropdownMenuItem | typeof ContextMenuItem;
}) {
  const copy = (text: string) => void navigator.clipboard?.writeText(text).catch(() => undefined);
  return (
    <>
      <Item icon={<SquarePlus />} onSelect={() => actions.onKeep(entry)}>
        Open in new tab
      </Item>
      <Item icon={<Columns2 />} onSelect={() => actions.onOpenBeside(entry)}>
        Open beside
      </Item>
      <Item icon={<Bot />} onSelect={() => actions.onAskAgent(entry)}>
        Ask agent about this entry point
      </Item>
      {/* Hidden rather than disabled when the API Client is not loaded: an
          intent nobody registered is a menu item that does nothing. */}
      {entry.kind === "http" && hasIntent("api.fromEndpoint") ? (
        <Item
          icon={<Send />}
          onSelect={() =>
            void dispatchIntent("api.fromEndpoint", {
              module: "codegraph",
              kind: "endpoint",
              id: entry.id,
              label: `${entry.verb} ${entry.target}`,
            })
          }
        >
          Try in API Client
        </Item>
      ) : null}
      <Item icon={<Copy />} onSelect={() => copy(`${entry.verb} ${entry.target}`)}>
        {entry.kind === "http" ? "Copy method and path" : "Copy verb and target"}
      </Item>
      {owner ? (
        <Item icon={<Copy />} onSelect={() => copy(owner)}>
          {entry.kind === "plugin" ? "Copy module name" : "Copy qualified name"}
        </Item>
      ) : null}
    </>
  );
}
