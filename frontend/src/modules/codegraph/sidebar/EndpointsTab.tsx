import { useMemo } from "react";
import { Bot, Boxes, CodeXml, Columns2, Copy, Ellipsis, SquarePlus } from "lucide-react";
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
import type { CodeGraphEndpoint } from "@/bridge/codegraph";
import { methodChipClass } from "../graph/tokens";
import { filterEndpoints, groupEndpoints, methodsIn, type ControllerGroup } from "./sidebarModel";

/** Figma `CodeGraph / Sidebar / Method Chip` (1189:41359): Geist Mono 10, radius 6. */
export const METHOD_CHIP = "inline-flex h-5.25 shrink-0 items-center justify-center rounded-sm px-1.5 text-code-11 font-semibold uppercase";

/** The filter strip's chips: ALL, and one per method present. */
const FILTER_CHIP = "inline-flex h-5.5 shrink-0 items-center rounded-sm border px-2 text-code-11";


type Props = {
  endpoints: CodeGraphEndpoint[];
  query: string;
  busy: boolean;
  selectedEndpointId: string | null;
  methodFilter: string[];
  collapsed: string[];
  onMethodFilter: (methods: string[]) => void;
  onToggleGroup: (key: string) => void;
  /** Single click: the flow in the preview tab. */
  onSelect: (ep: CodeGraphEndpoint) => void;
  /** Double click: the flow in a tab that stays. */
  onKeep: (ep: CodeGraphEndpoint) => void;
  /** The flow in a group beside the one in front. */
  onOpenBeside: (ep: CodeGraphEndpoint) => void;
  onAskAgent: (ep: CodeGraphEndpoint) => void;
};

/**
 * The endpoints, grouped by service and then controller
 * (Figma `CodeGraph / Sidebar — 1 Endpoints grouped`, 1189:65405).
 */
export function EndpointsTab({
  endpoints,
  query,
  busy,
  selectedEndpointId,
  methodFilter,
  collapsed,
  onMethodFilter,
  onToggleGroup,
  onSelect,
  onKeep,
  onOpenBeside,
  onAskAgent,
}: Props) {
  const methods = useMemo(() => methodsIn(endpoints), [endpoints]);
  const active = useMemo(() => new Set(methodFilter), [methodFilter]);
  const shown = useMemo(() => filterEndpoints(endpoints, query, active), [endpoints, query, active]);
  const groups = useMemo(() => groupEndpoints(shown), [shown]);
  // A search opens everything: a match inside a folded group is not a match.
  const searching = query.trim().length > 0 || active.size > 0;

  const menuActions = useMemo(() => ({ onKeep, onOpenBeside, onAskAgent }), [onKeep, onOpenBeside, onAskAgent]);
  const nodes = useMemo(() => {
    const closed = new Set(searching ? [] : collapsed);
    const endpointNode = (ep: CodeGraphEndpoint, controller: string): TreeNodeData => ({
      id: ep.id,
      label: (
        <>
          <span className={cn(METHOD_CHIP, "mr-2 w-12 align-middle", methodChipClass(ep.httpMethod))}>{ep.httpMethod}</span>
          {ep.path}
        </>
      ),
      title: `${ep.httpMethod} ${ep.path}`,
      selected: selectedEndpointId === ep.id,
      onSelect: () => onSelect(ep),
      onActivate: () => onKeep(ep),
      actions: (
        <>
          <Tooltip>
            <TooltipTrigger asChild>
              <TreeRowAction aria-label={`Ask agent about ${ep.httpMethod} ${ep.path}`} onClick={() => onAskAgent(ep)}>
                <Bot />
              </TreeRowAction>
            </TooltipTrigger>
            <TooltipContent>Ask agent</TooltipContent>
          </Tooltip>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <TreeRowAction aria-label={`More for ${ep.httpMethod} ${ep.path}`}>
                <Ellipsis />
              </TreeRowAction>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-60">
              <EndpointMenuItems endpoint={ep} controller={controller} actions={menuActions} Item={DropdownMenuItem} />
            </DropdownMenuContent>
          </DropdownMenu>
        </>
      ),
      wrap: (row) => (
        <ContextMenu key={`ep:${ep.id}`}>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <ContextMenuContent className="w-60">
            <EndpointMenuItems endpoint={ep} controller={controller} actions={menuActions} Item={ContextMenuItem} />
          </ContextMenuContent>
        </ContextMenu>
      ),
    });
    const controllerNode = (c: ControllerGroup, sticky: number): TreeNodeData => ({
      id: `ctl:${c.id}`,
      label: c.name,
      title: c.fqn,
      icon: <CodeXml />,
      count: c.endpoints.length,
      sticky,
      expandable: true,
      expanded: !closed.has(`ctl:${c.id}`),
      onToggle: () => onToggleGroup(`ctl:${c.id}`),
      children: c.endpoints.map((ep) => endpointNode(ep, c.fqn)),
    });
    // One service is no grouping at all: its controllers are the top level.
    if (groups.length === 1) return groups[0].controllers.map((c) => controllerNode(c, 0));
    return groups.map(
      (g): TreeNodeData => ({
        id: `svc:${g.key}`,
        label: g.label,
        icon: <Boxes />,
        count: g.count,
        sticky: 0,
        expandable: true,
        expanded: !closed.has(`svc:${g.key}`),
        onToggle: () => onToggleGroup(`svc:${g.key}`),
        children: g.controllers.map((c) => controllerNode(c, 1)),
      }),
    );
  }, [groups, searching, collapsed, selectedEndpointId, onSelect, onKeep, onAskAgent, onToggleGroup, menuActions]);

  const toggleMethod = (m: string) =>
    onMethodFilter(active.has(m) ? methodFilter.filter((x) => x !== m) : [...methodFilter, m]);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      {methods.length > 1 ? (
        <div className="flex flex-wrap items-center gap-1" role="group" aria-label="Filter by method">
          <button
            type="button"
            aria-pressed={active.size === 0}
            onClick={() => onMethodFilter([])}
            className={cn(
              FILTER_CHIP,
              active.size === 0
                ? "border-(--border-default) bg-surface-raised text-text-primary"
                : "border-border-subtle text-text-tertiary hover:text-text-secondary",
            )}
          >
            ALL
          </button>
          {methods.map((m) => (
            <button
              key={m}
              type="button"
              aria-pressed={active.has(m)}
              onClick={() => toggleMethod(m)}
              className={cn(
                METHOD_CHIP,
                methodChipClass(m),
                // Off chips recede rather than vanish: the strip keeps its shape.
                active.size > 0 && !active.has(m) && "opacity-40 hover:opacity-70",
                active.size === 0 && "opacity-70 hover:opacity-100",
              )}
            >
              {m}
            </button>
          ))}
        </div>
      ) : null}

      {nodes.length > 0 ? (
        <TreeView nodes={nodes} className="min-h-0 flex-1 scrollbar-gutter-stable" />
      ) : !busy ? (
        <p className="px-2 py-3 text-center text-body-12 text-text-tertiary">
          {endpoints.length === 0 ? "No endpoints found in this project." : "No endpoints match."}
        </p>
      ) : null}
    </div>
  );
}

type MenuActions = Pick<Props, "onKeep" | "onOpenBeside" | "onAskAgent">;

/**
 * What can be done with an endpoint without picking it first — on hover, and
 * the same list on right-click.
 */
function EndpointMenuItems({
  endpoint,
  controller,
  actions,
  Item,
}: {
  endpoint: CodeGraphEndpoint;
  controller: string;
  actions: MenuActions;
  Item: typeof DropdownMenuItem | typeof ContextMenuItem;
}) {
  const copy = (text: string) => void navigator.clipboard?.writeText(text).catch(() => undefined);
  return (
    <>
      <Item icon={<SquarePlus />} onSelect={() => actions.onKeep(endpoint)}>
        Open in new tab
      </Item>
      <Item icon={<Columns2 />} onSelect={() => actions.onOpenBeside(endpoint)}>
        Open beside
      </Item>
      <Item icon={<Bot />} onSelect={() => actions.onAskAgent(endpoint)}>
        Ask agent about this endpoint
      </Item>
      <Item icon={<Copy />} onSelect={() => copy(`${endpoint.httpMethod} ${endpoint.path}`)}>
        Copy method and path
      </Item>
      <Item icon={<Copy />} onSelect={() => copy(controller)}>
        Copy controller name
      </Item>
    </>
  );
}
