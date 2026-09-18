import { useMemo } from "react";
import { Bot, Boxes, ChevronRight, CodeXml, Copy, Ellipsis, Workflow } from "lucide-react";
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

const ACTION = "flex size-5 items-center justify-center rounded-sm text-text-tertiary hover:bg-state-soft hover:text-text-primary";

type Props = {
  endpoints: CodeGraphEndpoint[];
  query: string;
  busy: boolean;
  selectedEndpointId: string | null;
  methodFilter: string[];
  collapsed: string[];
  onMethodFilter: (methods: string[]) => void;
  onToggleGroup: (key: string) => void;
  onSelect: (ep: CodeGraphEndpoint) => void;
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
  onAskAgent,
}: Props) {
  const methods = useMemo(() => methodsIn(endpoints), [endpoints]);
  const active = useMemo(() => new Set(methodFilter), [methodFilter]);
  const shown = useMemo(() => filterEndpoints(endpoints, query, active), [endpoints, query, active]);
  const groups = useMemo(() => groupEndpoints(shown), [shown]);
  // A search opens everything: a match inside a folded group is not a match.
  const searching = query.trim().length > 0 || active.size > 0;
  const closed = new Set(searching ? [] : collapsed);
  const services = groups.length > 1;

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

      <div className="flex min-h-0 flex-1 flex-col gap-px overflow-y-auto" role="tree" aria-label="Endpoints">
        {groups.map((service) => {
          const serviceKey = `svc:${service.key}`;
          const serviceOpen = !services || !closed.has(serviceKey);
          return (
            <div key={service.key} className="flex flex-col gap-px" role="group">
              {services ? (
                <GroupHeader
                  level="service"
                  label={service.label}
                  count={service.count}
                  open={serviceOpen}
                  onToggle={() => onToggleGroup(serviceKey)}
                />
              ) : null}
              {serviceOpen
                ? service.controllers.map((c) => (
                    <ControllerBlock
                      key={c.id}
                      controller={c}
                      indent={services}
                      open={!closed.has(`ctl:${c.id}`)}
                      onToggle={() => onToggleGroup(`ctl:${c.id}`)}
                      selectedEndpointId={selectedEndpointId}
                      onSelect={onSelect}
                      onAskAgent={onAskAgent}
                    />
                  ))
                : null}
            </div>
          );
        })}
        {shown.length === 0 && !busy ? (
          <p className="px-2 py-3 text-center text-body-12 text-text-tertiary">
            {endpoints.length === 0 ? "No endpoints found in this project." : "No endpoints match."}
          </p>
        ) : null}
      </div>
    </div>
  );
}

/** Figma `CodeGraph / Sidebar / Group Header` (1189:65259). */
function GroupHeader({
  level,
  nested = false,
  label,
  count,
  open,
  onToggle,
}: {
  level: "service" | "controller";
  /** A controller under a service heading steps in one level. */
  nested?: boolean;
  label: string;
  count: number;
  open: boolean;
  onToggle: () => void;
}) {
  const Icon = level === "service" ? Boxes : CodeXml;
  return (
    <button
      type="button"
      role="treeitem"
      aria-expanded={open}
      onClick={onToggle}
      className={cn(
        "flex h-7 w-full shrink-0 items-center gap-1.5 rounded-md pr-2 text-left text-text-secondary hover:bg-state-soft",
        nested ? "pl-5.5" : "pl-2",
      )}
    >
      <ChevronRight className={cn("size-3 shrink-0 text-text-tertiary transition-transform", open && "rotate-90")} aria-hidden />
      <Icon className="size-3.5 shrink-0 text-text-tertiary" aria-hidden />
      <span className={cn("min-w-0 flex-1 truncate", level === "service" ? "text-label-11-caps" : "text-code-11")}>{label}</span>
      <span className="shrink-0 text-code-11 text-text-tertiary">{count}</span>
    </button>
  );
}

function ControllerBlock({
  controller,
  indent,
  open,
  onToggle,
  selectedEndpointId,
  onSelect,
  onAskAgent,
}: {
  controller: ControllerGroup;
  indent: boolean;
  open: boolean;
  onToggle: () => void;
  selectedEndpointId: string | null;
  onSelect: (ep: CodeGraphEndpoint) => void;
  onAskAgent: (ep: CodeGraphEndpoint) => void;
}) {
  return (
    <>
      <GroupHeader level="controller" nested={indent} label={controller.name} count={controller.endpoints.length} open={open} onToggle={onToggle} />
      {open
        ? controller.endpoints.map((ep) => (
            <EndpointRow
              key={ep.id}
              endpoint={ep}
              controller={controller.fqn}
              indent={indent}
              selected={selectedEndpointId === ep.id}
              onSelect={onSelect}
              onAskAgent={onAskAgent}
            />
          ))
        : null}
    </>
  );
}

/**
 * Figma `CodeGraph / Sidebar / Endpoint Row` (1189:65230). Hovered, it shows
 * what can be done with the endpoint without picking it first: ask the agent
 * about it, or the rest from its menu.
 */
function EndpointRow({
  endpoint,
  controller,
  indent,
  selected,
  onSelect,
  onAskAgent,
}: {
  endpoint: CodeGraphEndpoint;
  controller: string;
  indent: boolean;
  selected: boolean;
  onSelect: (ep: CodeGraphEndpoint) => void;
  onAskAgent: (ep: CodeGraphEndpoint) => void;
}) {
  const copy = (text: string) => void navigator.clipboard?.writeText(text).catch(() => undefined);
  return (
    <div
      role="treeitem"
      aria-selected={selected}
      className={cn(
        "group relative flex h-8.25 shrink-0 items-center rounded-md border",
        selected ? "border-border-subtle bg-surface-raised" : "border-transparent hover:bg-state-soft",
      )}
    >
      <button
        type="button"
        onClick={() => onSelect(endpoint)}
        className={cn("flex h-full min-w-0 flex-1 items-center gap-2.5 pr-2 text-left", indent ? "pl-10" : "pl-6.5")}
      >
        <span className={cn(METHOD_CHIP, "w-14.5", methodChipClass(endpoint.httpMethod))}>{endpoint.httpMethod}</span>
        <span className={cn("min-w-0 flex-1 truncate text-body-13", selected ? "text-text-primary" : "text-text-secondary")}>
          {endpoint.path}
        </span>
      </button>
      <div className="absolute right-1.5 flex items-center gap-0.5 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100">
        <Tooltip>
          <TooltipTrigger asChild>
            <button type="button" aria-label={`Ask agent about ${endpoint.httpMethod} ${endpoint.path}`} className={ACTION} onClick={() => onAskAgent(endpoint)}>
              <Bot className="size-3" />
            </button>
          </TooltipTrigger>
          <TooltipContent>Ask agent</TooltipContent>
        </Tooltip>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button type="button" aria-label={`More for ${endpoint.httpMethod} ${endpoint.path}`} className={ACTION}>
              <Ellipsis className="size-3" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-60">
            <DropdownMenuItem icon={<Workflow />} onSelect={() => onSelect(endpoint)}>
              Show call flow
            </DropdownMenuItem>
            <DropdownMenuItem icon={<Bot />} onSelect={() => onAskAgent(endpoint)}>
              Ask agent about this endpoint
            </DropdownMenuItem>
            <DropdownMenuItem icon={<Copy />} onSelect={() => copy(`${endpoint.httpMethod} ${endpoint.path}`)}>
              Copy method and path
            </DropdownMenuItem>
            <DropdownMenuItem icon={<Copy />} onSelect={() => copy(controller)}>
              Copy controller name
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
