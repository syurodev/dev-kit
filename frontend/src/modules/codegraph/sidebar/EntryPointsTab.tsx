import { SidebarInset } from "@/components/ui/sidebar-shell";
import { useCallback, useMemo } from "react";
import {
  Bot,
  Boxes,
  FolderGit2,
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
import { methodChipClass } from "@/lib/httpMethod";
import { filterEntryPoints, groupEntryPoints, verbsIn, type KindGroup, type OwnerGroup } from "./sidebarModel";
import { DOT } from "./WorkspaceHeader";
import type { FolderHealth } from "../workspace/indexHealth";
import { entryKey } from "../workspace/identity";

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

/** One folder's entry points and how well they can be trusted. */
export type FolderEntries = {
  folderId: string;
  /** The folder's registered path: names its index to the other modules that read it. */
  path: string;
  label: string;
  entries: CodeGraphEntryPoint[];
  health: FolderHealth;
  /** Why the list is empty or old, when it is. */
  note: string;
};

type Props = {
  folders: FolderEntries[];
  query: string;
  selected: { folderId: string; entryPointId: string } | null;
  verbFilter: string[];
  collapsed: string[];
  onVerbFilter: (verbs: string[]) => void;
  onToggleGroup: (key: string) => void;
  /** Single click: the flow in the preview tab. */
  onSelect: (folderId: string, ep: CodeGraphEntryPoint) => void;
  /** Double click: the flow in a tab that stays. */
  onKeep: (folderId: string, ep: CodeGraphEntryPoint) => void;
  /** The flow in a group beside the one in front. */
  onOpenBeside: (folderId: string, ep: CodeGraphEntryPoint) => void;
  onAskAgent: (folderId: string, ep: CodeGraphEntryPoint) => void;
  onReindexFolder: (folderId: string) => void;
};

/**
 * The entry points of the folders in view — HTTP routes, Kong plugin phases,
 * message handlers, scheduled jobs — grouped by folder, then kind, service and
 * what declares them (Figma `CodeGraph / Sidebar — 1 Endpoints grouped`,
 * extended with the folder level). A level with a single group is skipped: one
 * heading over everything says nothing. An entry point is its folder's: the same
 * `GET /health` in two folders is two rows under two headings, and a folder whose
 * index is old, still building, failed or unsupported says so on its own heading
 * instead of silently contributing nothing.
 */
export function EntryPointsTab({
  folders,
  query,
  selected,
  verbFilter,
  collapsed,
  onVerbFilter,
  onToggleGroup,
  onSelect,
  onKeep,
  onOpenBeside,
  onAskAgent,
  onReindexFolder,
}: Props) {
  const all = useMemo(() => folders.flatMap((f) => f.entries), [folders]);
  const verbs = useMemo(() => verbsIn(all), [all]);
  const active = useMemo(() => new Set(verbFilter), [verbFilter]);
  const shown = useMemo(
    () => folders.map((f) => ({ ...f, groups: groupEntryPoints(filterEntryPoints(f.entries, query, active)), count: filterEntryPoints(f.entries, query, active).length })),
    [folders, query, active],
  );
  // A search opens everything: a match inside a folded group is not a match.
  const searching = query.trim().length > 0 || active.size > 0;

  const menuActions = useMemo(() => ({ onKeep, onOpenBeside, onAskAgent }), [onKeep, onOpenBeside, onAskAgent]);
  const pathOf = useCallback((folderId: string) => folders.find((f) => f.folderId === folderId)?.path ?? "", [folders]);
  const nodes = useMemo(() => {
    const closed = new Set(searching ? [] : collapsed);
    const entryNode = (folderId: string, ep: CodeGraphEntryPoint, owner: string): TreeNodeData => ({
      id: entryKey(folderId, ep.id),
      label: (
        <>
          {/* min-w, not w: a Kong phase (BODY_FILTER, HEADER_FILTER) is
              longer than an HTTP method and needs to grow past the 48px an
              HTTP method chip sizes to, or its own text overflows the box
              uncontained (no overflow-hidden on the chip) and bleeds into the
              row's truncate boundary and the label beside it. */}
          <span className={cn(VERB_CHIP, "mr-2 min-w-12 align-middle", methodChipClass(ep.verb))}>{ep.verb}</span>
          {/* Under its plugin, a phase's target is the plugin again: the
              chip alone says which phase it is. */}
          {ep.kind === "plugin" && ep.target === ep.group ? null : ep.routeId || ep.target}
          {ep.routeId || ep.consumerGroup ? <span className="ml-2 text-text-tertiary">{ep.routeId ? ep.target : ""}{ep.consumerGroup ? ` · group ${ep.consumerGroup}` : ""}</span> : null}
        </>
      ),
      title: [ep.routeId, `${ep.verb} ${ep.target}`, ep.consumerGroup ? `Consumer group: ${ep.consumerGroup}` : ""].filter(Boolean).join(" · "),
      selected: selected?.folderId === folderId && selected.entryPointId === ep.id,
      onSelect: () => onSelect(folderId, ep),
      onActivate: () => onKeep(folderId, ep),
      actions: (
        <>
          <Tooltip>
            <TooltipTrigger asChild>
              <TreeRowAction aria-label={`Ask agent about ${ep.verb} ${ep.target}`} onClick={() => onAskAgent(folderId, ep)}>
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
              <EntryPointMenuItems folderId={folderId} projectPath={pathOf(folderId)} entry={ep} owner={owner} actions={menuActions} Item={DropdownMenuItem} />
            </DropdownMenuContent>
          </DropdownMenu>
        </>
      ),
      wrap: (row) => (
        <ContextMenu key={`ep:${folderId}:${ep.id}`}>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <ContextMenuContent className="w-60">
            <EntryPointMenuItems folderId={folderId} projectPath={pathOf(folderId)} entry={ep} owner={owner} actions={menuActions} Item={ContextMenuItem} />
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
    const build = (f: (typeof shown)[number], base: number): TreeNodeData[] => {
      const ownerNode = (o: OwnerGroup, kind: string, sticky: number) =>
        group(`${f.folderId}:${o.key}`, o.label, o.owner, kind === "plugin" ? Puzzle : CodeXml, o.entries.length, sticky, o.entries.map((ep) => entryNode(f.folderId, ep, o.owner)));
      const serviceNodes = (k: KindGroup, sticky: number): TreeNodeData[] =>
        k.services.length === 1
          ? k.services[0].owners.map((o) => ownerNode(o, k.kind, sticky))
          : k.services.map((svc) =>
              group(`${f.folderId}:${svc.key}`, svc.label || "default", undefined, Boxes, svc.count, sticky, svc.owners.map((o) => ownerNode(o, k.kind, sticky + 1))),
            );
      if (f.groups.length === 1) return serviceNodes(f.groups[0], base);
      return f.groups.map((k) => group(`${f.folderId}:${k.key}`, k.label, undefined, kindIcon(k.kind), k.count, base, serviceNodes(k, base + 1)));
    };

    // One folder in view with entries: its groups directly. Otherwise a heading
    // per folder, so a row is never without its owner.
    if (shown.length === 1 && shown[0].count > 0) return build(shown[0], 0);
    return shown
      .filter((f) => !searching || f.count > 0)
      .map((f): TreeNodeData => ({
        id: `folder:${f.folderId}`,
        label: f.label,
        icon: <FolderGit2 />,
        count: f.count,
        sticky: 0,
        meta: f.health.health === "ready" ? undefined : <span title={f.health.detail}>{f.health.label}</span>,
        trailing: <span className={cn("size-1.5 shrink-0 rounded-full", DOT[f.health.tone])} title={`${f.health.label}. ${f.health.detail}`} aria-label={f.health.label} />,
        expandable: true,
        expanded: !closed.has(`folder:${f.folderId}`),
        onToggle: () => onToggleGroup(`folder:${f.folderId}`),
        emptyLabel: f.count === 0 ? <FolderNote note={f.note || (f.entries.length === 0 ? `No supported entry points found. ${f.health.detail}` : "No entry points match")} health={f.health} onReindex={() => onReindexFolder(f.folderId)} /> : undefined,
        children: build(f, 1),
      }));
  }, [shown, searching, collapsed, selected, onSelect, onKeep, onAskAgent, onToggleGroup, onReindexFolder, menuActions, pathOf]);

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
                // A filtered-out chip turns neutral rather than vanishing, so
                // the strip keeps its shape — never a faded tint: the hue on
                // its tint is 4.5:1 only at full strength (themeContrast.test).
                active.size > 0 && !active.has(v)
                  ? "bg-surface-raised text-text-tertiary hover:text-text-secondary"
                  : methodChipClass(v),
              )}
            >
              {v}
            </button>
          ))}
        </SidebarInset>
      ) : null}

      {folders.some((f) => f.health.health === "limited" && f.entries.length > 0) ? (
        <SidebarInset className="text-body-12 text-text-secondary" role="status">
          {folders.filter((f) => f.health.health === "limited" && f.entries.length > 0).map((f) => (
            <details key={f.folderId}>
              <summary>{f.label}: indexed with entry point limits</summary>
              <p>{f.health.detail}</p>
            </details>
          ))}
        </SidebarInset>
      ) : null}

      {nodes.length > 0 ? (
        <TreeView nodes={nodes} className="min-h-0 flex-1 scrollbar-gutter-stable" />
      ) : (
        <p className="px-2 py-3 text-center text-body-12 text-text-tertiary">
          {folders.length === 0 ? "No folders in view." : all.length === 0 ? "No supported entry points found. Index completion does not imply support for every framework or runtime route." : "No entry points match."}
        </p>
      )}
    </div>
  );
}

type MenuActions = Pick<Props, "onKeep" | "onOpenBeside" | "onAskAgent">;

/** Why a folder has no rows, and what can be done about it. */
function FolderNote({ note, health, onReindex }: { note: string; health: FolderHealth; onReindex: () => void }) {
  const canRetry = health.health === "failed" || health.health === "not-indexed" || health.health === "needs-analyzer";
  // One line: the note sits in a fixed-height tree row, and wrapped it spilled
  // over the folders below. The whole of it is in the tooltip, and the action
  // never truncates away.
  const full = !health.detail || note.includes(health.detail) ? note : `${note}\n${health.detail}`;
  return (
    <span className="flex min-w-0 items-center gap-1">
      <span className="min-w-0 truncate" title={full}>
        {note}
      </span>
      {canRetry ? (
        <>
          <span aria-hidden>·</span>
          <button type="button" className="shrink-0 underline underline-offset-2 hover:text-text-primary" onClick={onReindex}>
            {health.health === "failed" ? "Retry" : "Index now"}
          </button>
        </>
      ) : null}
    </span>
  );
}

/**
 * What can be done with an entry point without picking it first — on hover,
 * and the same list on right-click. Only an HTTP route is a request the API
 * Client can send.
 */
function EntryPointMenuItems({
  folderId,
  projectPath,
  entry,
  owner,
  actions,
  Item,
}: {
  folderId: string;
  projectPath: string;
  entry: CodeGraphEntryPoint;
  owner: string;
  actions: MenuActions;
  Item: typeof DropdownMenuItem | typeof ContextMenuItem;
}) {
  const copy = (text: string) => void navigator.clipboard?.writeText(text).catch(() => undefined);
  return (
    <>
      <Item icon={<SquarePlus />} onSelect={() => actions.onKeep(folderId, entry)}>
        Open in new tab
      </Item>
      <Item icon={<Columns2 />} onSelect={() => actions.onOpenBeside(folderId, entry)}>
        Open beside
      </Item>
      <Item icon={<Bot />} onSelect={() => actions.onAskAgent(folderId, entry)}>
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
              // The endpoint id is its folder's: name the index it came from.
              project: projectPath,
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
