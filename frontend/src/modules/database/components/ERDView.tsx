import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ReactFlow,
  useNodesState,
  useEdgesState,
  useNodesInitialized,
  useReactFlow,
  Background,
  Controls,
  MiniMap,
  Handle,
  Position,
  type Node,
  type Edge,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  Network,
  Table as TableIcon,
  Key,
  Columns,
  Link as LinkIcon,
  ChevronRight,
  ChevronDown,
  ChevronsUpDown,
  ChevronsDownUp,
  Crosshair,
  Database,
  Info,
  AlertTriangle,
  Spline,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { useIsActiveDocument } from "../useIsActiveDocument";
import { useTheme } from "@/theme/ThemeProvider";
import { errorMessage } from "@/bridge/errors";
import {
  erdTableWindow,
  hasSingleIdPrimaryKey,
  erdColumnWindow,
  ERD_EDGE_LIMIT,
  ERD_COLUMN_LIMIT,
} from "./erdBudget";
import { Button } from "@/components/ui/button";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import {
  listForeignKeys,
  listIndexes,
  type ForeignKey,
} from "../../../bridge/database";
import { EMPTY_ARRAY, EMPTY_OBJECT } from "@/lib/stableEmpty";
import { useDatabaseChrome } from "../useDatabaseChrome";

/**
 * The naming heuristic needs types, not just names. Two columns are treated as
 * linkable when they sit in the same family — an int cannot reference a uuid,
 * and matching names across families is exactly the false positive the design
 * page warns about.
 */
function typeFamily(dataType: string): string {
  const t = dataType.toLowerCase();
  if (t.includes("uuid")) return "uuid";
  if (
    t.includes("int") ||
    t.includes("serial") ||
    t.includes("numeric") ||
    t.includes("decimal")
  ) {
    return "int";
  }
  if (t.includes("char") || t.includes("text")) return "text";
  return t;
}

type TableNodeData = {
  tableName: string;
  schema: string;
  columns: string[];
  foreignKeys: ForeignKey[];
  inferredRefs: Array<{ fromCol: string; toTable: string; toCol: string }>;
  // Columns an edge actually lands on. A collapsed node keeps exactly these:
  // hide the column and its Handle goes with it, and every edge pointing at it
  // silently stops rendering.
  keyColumns: string[];
  // The real primary key, as the engine reports it. A column named `id` is not
  // the same claim, and plenty of tables here key on something else.
  primaryColumns: string[];
  columnTypes: Record<string, string>;
  isJunction: boolean;
  isFocused: boolean;
  expanded: boolean;
  onToggle: () => void;
};

/**
 * Postgres reports "character varying" and "timestamp without time zone" where
 * MySQL already says varchar and timestamp. In a node that is 300px wide the
 * long spellings push the column name out; the short one is what a developer
 * writes anyway.
 */
function shortType(dataType: string): string {
  const t = dataType.toLowerCase();
  if (t.startsWith("character varying")) return "varchar";
  if (t === "character") return "char";
  if (t.startsWith("timestamp"))
    return t.includes("with time zone") ? "timestamptz" : "timestamp";
  if (t.startsWith("time "))
    return t.includes("with time zone") ? "timetz" : "time";
  if (t === "double precision") return "double";
  if (t === "boolean") return "bool";
  return t.replace(/ unsigned$/, "");
}

function visibleColumns(data: TableNodeData): string[] {
  return erdColumnWindow(data.columns, data.keyColumns, data.expanded);
}

function TableNode({ data }: { data: TableNodeData }) {
  return (
    <div
      className={`bg-surface-raised rounded-xl shadow-lg min-w-65 max-w-85 overflow-hidden text-code-12 border ${
        data.isFocused
          ? "border-action-primary ring-1 ring-action-primary/40"
          : "border-border/80"
      }`}
    >
      {/* Node Header — doubles as the expand toggle */}
      <button
        type="button"
        onClick={data.onToggle}
        className="w-full px-3 py-2 bg-surface-base border-b border-border flex items-center justify-between gap-2 hover:bg-state-soft text-left"
      >
        <div className="flex items-center gap-1.5 truncate">
          {data.expanded ? (
            <ChevronDown className="w-3 h-3 text-text-tertiary shrink-0" />
          ) : (
            <ChevronRight className="w-3 h-3 text-text-tertiary shrink-0" />
          )}
          <TableIcon className="w-3.5 h-3.5 text-action-primary shrink-0" />
          <span className="font-semibold text-text-primary truncate">
            {data.tableName}
          </span>
        </div>
        {data.isJunction && (
          <span
            className="text-caption-11 text-status-info border border-status-info/40 px-1.5 rounded shrink-0"
            title="Primary key is exactly this table's reference columns — it joins two tables many-to-many"
          >
            N–N
          </span>
        )}
        <span className="text-caption-11 text-text-tertiary font-sans bg-surface-raised px-1.5 py-0.5 rounded">
          {data.expanded
            ? `${Math.min(data.columns.length, ERD_COLUMN_LIMIT)}/${data.columns.length} cols`
            : `${visibleColumns(data).length}/${data.columns.length}`}
        </span>
      </button>

      {data.expanded && data.columns.length > ERD_COLUMN_LIMIT ? (
        <p className="px-3 py-1 text-caption-11 text-text-secondary">
          Column preview capped at 80; relation keys come first.
        </p>
      ) : null}
      {/* Columns List */}
      {/* nowheel lets the list scroll instead of the canvas zooming. A 32
          column table would otherwise be a wall five times taller than
          everything beside it. */}
      <div className="nowheel p-2 space-y-1 bg-surface-raised/40 max-h-80 overflow-y-auto">
        {visibleColumns(data).map((col) => {
          const isPK = data.primaryColumns.includes(col);
          const isDeclaredFK = data.foreignKeys.some((fk) => fk.column === col);
          const isInferredFK = data.inferredRefs.some(
            (inf) => inf.fromCol === col,
          );

          return (
            <div
              key={col}
              className="relative flex items-center justify-between py-0.5 px-1.5 rounded hover:bg-surface-base/80 text-caption-11"
            >
              {/* Target handle for incoming relations */}
              <Handle
                type="target"
                position={Position.Left}
                id={`${data.tableName}-${col}-target`}
                className="w-2 h-2 !bg-action-primary opacity-0 hover:opacity-100"
              />

              <div className="flex items-center gap-1.5 truncate">
                {isPK ? (
                  <Key className="w-3 h-3 text-status-warning shrink-0" />
                ) : isDeclaredFK ? (
                  <LinkIcon className="w-3 h-3 text-status-info shrink-0" />
                ) : isInferredFK ? (
                  <LinkIcon className="w-3 h-3 text-text-tertiary shrink-0" />
                ) : (
                  <Columns className="w-3 h-3 text-text-tertiary shrink-0" />
                )}
                <span
                  className={`truncate ${
                    isPK
                      ? "text-status-warning font-semibold"
                      : isDeclaredFK
                        ? "text-status-info font-medium"
                        : "text-text-primary"
                  }`}
                >
                  {col}
                </span>
              </div>

              <div className="flex items-center gap-1 shrink-0">
                <span className="text-code-11 text-text-tertiary">
                  {data.columnTypes[col] ?? ""}
                </span>
                {isPK ? (
                  <span className="text-caption-11 text-status-warning border border-status-warning/40 px-1 rounded">
                    PK
                  </span>
                ) : isDeclaredFK ? (
                  <span className="text-caption-11 text-status-info border border-status-info/40 px-1 rounded">
                    FK
                  </span>
                ) : isInferredFK ? (
                  <span className="text-caption-11 text-text-tertiary bg-surface-base px-1 rounded">
                    inf
                  </span>
                ) : null}
              </div>

              {/* Source handle for outgoing relations */}
              <Handle
                type="source"
                position={Position.Right}
                id={`${data.tableName}-${col}-source`}
                className="w-2 h-2 !bg-action-primary opacity-0 hover:opacity-100"
              />
            </div>
          );
        })}
      </div>
    </div>
  );
}

const nodeTypes = {
  tableNode: TableNode,
};

/**
 * Crow's-foot markers, one set per edge colour.
 *
 * SVG markers do not inherit the path's stroke, and `context-stroke` is not
 * safe across every webview this app ships in, so each variant is emitted with
 * its colour baked in. The marker's +x axis runs along the path, which for a
 * start marker means away from the child table: the prongs sit against the
 * table and the optionality symbol reads just outside them.
 */
// Above this many visible tables the edges stop animating. Roughly where a
// focused view ends and browsing a whole schema begins.
const ANIMATED_NODE_LIMIT = 30;

// How tall an expanded node's column list may grow before it scrolls itself.
const MAX_NODE_BODY = 320;

const EDGE_VARIANTS = [
  { id: "declared", color: "var(--status-info)" },
  { id: "inferred", color: "var(--text-tertiary)" },
] as const;

/** Shared shape for the control bar's selects — native behaviour, designed
 *  chrome. `appearance-none` drops the platform arrow, so one is drawn back. */
function ControlSelect({
  value,
  onChange,
  children,
  icon,
  title,
}: {
  value: string;
  onChange: (value: string) => void;
  children: React.ReactNode;
  icon?: React.ReactNode;
  title?: string;
}) {
  return (
    <div className="relative flex items-center h-8 rounded-lg border border-border bg-surface-base focus-within:border-action-primary">
      {icon && <span className="pl-2.5 shrink-0">{icon}</span>}
      <BareSelect value={value} onChange={onChange} title={title}>
        {children}
      </BareSelect>
    </div>
  );
}

function BareSelect({
  value,
  onChange,
  children,
  className = "",
  title,
}: {
  value: string;
  onChange: (value: string) => void;
  children: React.ReactNode;
  className?: string;
  title?: string;
}) {
  return (
    <NativeSelect
      value={value}
      onChange={(e) => onChange(e.target.value)}
      aria-label={title}
      title={title}
      className={`h-8 ${className}`}
    >
      {children}
    </NativeSelect>
  );
}

/** Filter state is local to this control; table-name typing never relayouts the graph. */
function TableFocusSelect({
  tables,
  value,
  onChange,
}: {
  tables: Record<string, string[]>;
  value: string;
  onChange: (value: string) => void;
}) {
  const [search, setSearch] = useState("");
  const matches = Object.keys(tables)
    .sort()
    .filter((name) => name.toLowerCase().includes(search.trim().toLowerCase()));
  const options = matches.slice(0, 200);
  if (value && !options.includes(value)) options.unshift(value);
  return (
    <>
      <Input
        aria-label="Filter focus tables"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        placeholder="Find table…"
        className="h-8 w-32 border-0"
      />
      <BareSelect
        value={value}
        onChange={onChange}
        className="max-w-60"
        title="Focus on one table and its neighbours"
      >
        <option value="">All tables</option>
        {options.map((name) => (
          <option key={name} value={name}>
            {name}
          </option>
        ))}
        {matches.length > 200 ? (
          <option disabled>Refine filter for more tables</option>
        ) : null}
        {!matches.length && search ? (
          <option disabled>No matching tables</option>
        ) : null}
      </BareSelect>
    </>
  );
}

function ERDMarkers() {
  return (
    <svg className="absolute w-0 h-0" aria-hidden>
      <defs>
        {EDGE_VARIANTS.map(({ id, color }) => {
          const line = { stroke: color, strokeWidth: 1.5, fill: "none" };
          return (
            <g key={id}>
              {/* many, optional — 0..N */}
              <marker
                id={`erd-${id}-crow-opt`}
                viewBox="0 0 22 14"
                refX="0"
                refY="7"
                markerWidth="22"
                markerHeight="14"
                markerUnits="userSpaceOnUse"
                orient="auto"
              >
                <path d="M0,1 L10,7 M0,7 L10,7 M0,13 L10,7" style={line} />
                <circle cx="16" cy="7" r="3.5" style={line} />
              </marker>

              {/* many, required — 1..N */}
              <marker
                id={`erd-${id}-crow-req`}
                viewBox="0 0 20 14"
                refX="0"
                refY="7"
                markerWidth="20"
                markerHeight="14"
                markerUnits="userSpaceOnUse"
                orient="auto"
              >
                <path
                  d="M0,1 L10,7 M0,7 L10,7 M0,13 L10,7 M15,2 L15,12"
                  style={line}
                />
              </marker>

              {/* one, optional — 0..1 */}
              <marker
                id={`erd-${id}-one-opt`}
                viewBox="0 0 20 14"
                refX="0"
                refY="7"
                markerWidth="20"
                markerHeight="14"
                markerUnits="userSpaceOnUse"
                orient="auto"
              >
                <path d="M5,2 L5,12" style={line} />
                <circle cx="13" cy="7" r="3.5" style={line} />
              </marker>

              {/* one, required — exactly 1 */}
              <marker
                id={`erd-${id}-one-req`}
                viewBox="0 0 16 14"
                refX="0"
                refY="7"
                markerWidth="16"
                markerHeight="14"
                markerUnits="userSpaceOnUse"
                orient="auto"
              >
                <path d="M5,2 L5,12 M11,2 L11,12" style={line} />
              </marker>

              {/* the parent end is always exactly one */}
              <marker
                id={`erd-${id}-one`}
                viewBox="0 0 14 14"
                refX="14"
                refY="7"
                markerWidth="14"
                markerHeight="14"
                markerUnits="userSpaceOnUse"
                orient="auto"
              >
                <path d="M9,2 L9,12" style={line} />
              </marker>
            </g>
          );
        })}
      </defs>
    </svg>
  );
}

/**
 * Refit the viewport when the visible set of tables changes — and only then, so
 * expanding a node leaves the user's zoom alone.
 *
 * Timing is the whole problem here: fitView needs measured nodes, and guessing
 * at that with a frame or two either fires too early or gets cancelled by the
 * very state update it was waiting for. useNodesInitialized reports the fact
 * instead of guessing. Rendered inside ReactFlow to reach its context.
 */
function FitOnFocusChange({ trigger }: { trigger: string }) {
  const initialized = useNodesInitialized();
  const { fitView } = useReactFlow();
  const fitted = useRef("");

  useEffect(() => {
    if (!initialized || fitted.current === trigger) return;
    fitted.current = trigger;
    // Never fit past 1:1. A two-table focus would otherwise be blown up to
    // maxZoom, and the browser rasterises the viewport layer before scaling it
    // — the nodes come out blurry until something forces a repaint.
    void fitView({ duration: 200, maxZoom: 1 });
  }, [initialized, trigger, fitView]);

  return null;
}

/**
 * Resolve which table a `<x>_id` column points at, by name alone.
 *
 * Schemas that namespace their tables (crm_leads, his_billing_statements) never
 * match a bare stem: crm_leads.customer_id means crm_customers, not customers.
 * The source table's own prefix is tried first — a reference inside a module is
 * far likelier than one across modules. Returns "" when nothing matches, which
 * is the point: a wrong edge is worse than a missing one.
 */
export function resolveRefTarget(
  tableName: string,
  column: string,
  tableNames: string[],
): string {
  if (!column.endsWith("_id") || column === "id") return "";
  const stem = column.slice(0, -3);
  const underscore = tableName.indexOf("_");
  const prefix = underscore > 0 ? tableName.slice(0, underscore + 1) : "";
  const candidates = [
    prefix && `${prefix}${stem}`,
    prefix && `${prefix}${stem}s`,
    stem,
    `${stem}s`,
  ].filter(Boolean) as string[];
  const hit = candidates.find(
    (name) => name !== tableName && tableNames.includes(name),
  );
  return hit ?? "";
}

export type NodeSpec = { tableName: string; height: number };

/**
 * Lay a focused graph out as one column per hop.
 *
 * A focus is a star: one table in the middle and its neighbours around it.
 * Packed into a grid the centre lands wherever it fits and every edge crosses
 * the canvas, which is what makes a hub table unreadable. One column per hop
 * puts the relations left to right instead.
 *
 * Within a column the order follows where each table's neighbours sit in the
 * column before it, so edges between two columns mostly stay parallel rather
 * than braiding. Columns are centred against the tallest one.
 */
export function placeByHop(
  specs: NodeSpec[],
  hops: Record<string, number>,
  neighbours: Record<string, Set<string>>,
  { nodeWidth, gap }: { nodeWidth: number; gap: number },
): Map<string, { x: number; y: number }> {
  const byHop = new Map<number, NodeSpec[]>();
  for (const spec of specs) {
    const hop = hops[spec.tableName] ?? 0;
    const list = byHop.get(hop);
    if (list) list.push(spec);
    else byHop.set(hop, [spec]);
  }

  const order = new Map<string, number>();
  const hopKeys = [...byHop.keys()].sort((a, b) => a - b);
  for (const hop of hopKeys) {
    const members = byHop.get(hop)!;
    if (hop > 0) {
      const anchor = (name: string) => {
        const parents = [...(neighbours[name] || [])]
          .map((n) => order.get(n))
          .filter((v): v is number => v !== undefined);
        return parents.length ? Math.min(...parents) : Number.MAX_SAFE_INTEGER;
      };
      members.sort(
        (a, b) =>
          anchor(a.tableName) - anchor(b.tableName) ||
          a.tableName.localeCompare(b.tableName),
      );
    }
    members.forEach((spec, i) => order.set(spec.tableName, i));
  }

  const columnHeights = hopKeys.map((hop) =>
    byHop.get(hop)!.reduce((sum, spec) => sum + spec.height + gap, -gap),
  );
  const tallest = Math.max(...columnHeights, 0);

  const positions = new Map<string, { x: number; y: number }>();
  hopKeys.forEach((hop, colIdx) => {
    let y = 50 + (tallest - columnHeights[colIdx]) / 2;
    for (const spec of byHop.get(hop)!) {
      positions.set(spec.tableName, { x: colIdx * nodeWidth + 50, y });
      y += spec.height + gap;
    }
  });
  return positions;
}

export function ERDView() {
  useDatabaseChrome();
  const active = useIsActiveDocument("database.erd");
  const { resolvedTheme } = useTheme();
  // Schema and focused table live in the store, not here: the sidebar picks a
  // table by clicking it, which is the natural gesture at 161 tables, and the
  // dropdown is just a second way into the same state.
  const {
    selectedProfileId,
    schemaSnapshot,
    selectedSchema,
    setSelectedSchema,
    selectedTable: focusTable,
    setSelectedTable: setFocusTable,
  } = useDatabaseStore(
    useShallow((s) => ({
      selectedProfileId: s.selectedProfileId,
      schemaSnapshot: s.schemaSnapshot,
      selectedSchema: s.selectedSchema,
      setSelectedSchema: s.setSelectedSchema,
      selectedTable: s.selectedTable,
      setSelectedTable: s.setSelectedTable,
    })),
  );
  const schemas = useMemo(() => {
    return Object.keys(schemaSnapshot?.namespace || {});
  }, [schemaSnapshot]);

  useEffect(() => {
    if (schemas.length > 0 && !schemas.includes(selectedSchema)) {
      setSelectedSchema(schemas[0]);
    }
  }, [schemas, selectedSchema]);

  // Load declared Foreign Keys
  const fkQuery = useQuery({
    queryKey: ["database", "erd", "fks", selectedProfileId, selectedSchema],
    queryFn: async () => {
      if (!selectedProfileId || !selectedSchema) return [];
      return listForeignKeys(selectedProfileId, selectedSchema);
    },
    enabled: active && Boolean(selectedProfileId && selectedSchema),
  });

  // Indexes answer whether a relation is one-to-one or one-to-many.
  const indexQuery = useQuery({
    queryKey: ["database", "erd", "indexes", selectedProfileId, selectedSchema],
    queryFn: async () => {
      if (!selectedProfileId || !selectedSchema) return [];
      return listIndexes(selectedProfileId, selectedSchema);
    },
    enabled: active && Boolean(selectedProfileId && selectedSchema),
  });

  const tablesInSchema = useMemo(() => {
    if (
      !schemaSnapshot?.namespace ||
      !schemaSnapshot.namespace[selectedSchema]
    ) {
      return {};
    }
    return schemaSnapshot.namespace[selectedSchema] || {};
  }, [schemaSnapshot, selectedSchema]);

  const declaredFKs = fkQuery.data ?? EMPTY_ARRAY;
  const indexes = indexQuery.data ?? EMPTY_ARRAY;
  // IndexSchema already supplies typed columns; avoid one bridge request per table.
  const typedColumns =
    schemaSnapshot?.columns?.[selectedSchema] ?? EMPTY_OBJECT;
  const metadataError = fkQuery.error || indexQuery.error;
  const metadataPending = fkQuery.isFetching || indexQuery.isFetching;

  const metadataReady =
    fkQuery.data !== undefined && indexQuery.data !== undefined;
  // Compute Inferred Relationships (<x>_id -> <x>.id or <x>s.id)
  const inferredRelationships = useMemo(() => {
    if (!metadataReady) return [];
    const tableNames = Object.keys(tablesInSchema);
    const inferred: Array<{
      fromTable: string;
      fromCol: string;
      toTable: string;
      toCol: string;
    }> = [];

    for (const [tableName, columns] of Object.entries(tablesInSchema)) {
      if (!columns) continue;
      for (const col of columns) {
        // Skip if already in declared FKs
        const isDeclared = declaredFKs.some(
          (fk) => fk.table === tableName && fk.column === col,
        );
        if (isDeclared) continue;

        if (col.endsWith("_id") && col !== "id") {
          const targetTable = resolveRefTarget(tableName, col, tableNames);

          // A matching table name is not enough. The design page requires the
          // target to actually have a single-column `id` primary key whose type
          // family matches — without this the diagram draws edges to tables
          // that have no id at all.
          const sourceColumn = (typedColumns[tableName] || []).find(
            (c) => c.name === col,
          );
          const targetID = (typedColumns[targetTable] || []).find(
            (c) => c.name === "id",
          );
          const typesLineUp =
            !!sourceColumn &&
            !!targetID &&
            typeFamily(sourceColumn.dataType) === typeFamily(targetID.dataType);

          const targetHasIdKey = hasSingleIdPrimaryKey(indexes, targetTable);
          if (
            targetTable &&
            targetTable !== tableName &&
            typesLineUp &&
            targetHasIdKey
          ) {
            inferred.push({
              fromTable: tableName,
              fromCol: col,
              toTable: targetTable,
              toCol: "id",
            });
          }
        }
      }
    }
    return inferred;
  }, [tablesInSchema, declaredFKs, typedColumns, indexes, metadataReady]);

  // Every relation in one shape, used for three things: the key-column set a
  // collapsed node must keep, the adjacency focus mode walks, and nothing else.
  const relations = useMemo(
    () => [
      ...declaredFKs.map((fk) => ({
        from: fk.table,
        fromCol: fk.column,
        to: fk.referencedTable,
        toCol: fk.referencedColumn,
      })),
      ...inferredRelationships.map((inf) => ({
        from: inf.fromTable,
        fromCol: inf.fromCol,
        to: inf.toTable,
        toCol: inf.toCol,
      })),
    ],
    [declaredFKs, inferredRelationships],
  );

  // Two facts, both straight from the engine rather than from a name.
  const { uniqueSingleColumns, primaryColumnsByTable } = useMemo(() => {
    const unique = new Set<string>();
    const primary: Record<string, string[]> = {};
    for (const idx of indexes) {
      if (idx.primary) primary[idx.table] = idx.columns;
      // Only a single-column unique index proves the column itself is unique.
      // A unique index on (tenant_id, email) says nothing about either alone.
      if (idx.unique && idx.columns.length === 1) {
        unique.add(`${idx.table}\u0000${idx.columns[0]}`);
      }
    }
    return { uniqueSingleColumns: unique, primaryColumnsByTable: primary };
  }, [indexes]);

  // A junction table is one whose primary key is exactly the columns it uses to
  // reference other tables — that is what makes the two tables it sits between
  // a many-to-many. It stays on the diagram as its own node: collapsing it into
  // a single edge would hide a table that usually carries its own data.
  const junctionTables = useMemo(() => {
    const refCols: Record<string, Set<string>> = {};
    for (const rel of relations) {
      (refCols[rel.from] || (refCols[rel.from] = new Set())).add(rel.fromCol);
    }
    const junctions = new Set<string>();
    for (const [table, cols] of Object.entries(refCols)) {
      const pk = primaryColumnsByTable[table];
      if (!pk || pk.length < 2) continue;
      if (pk.length === cols.size && pk.every((c) => cols.has(c)))
        junctions.add(table);
    }
    return junctions;
  }, [relations, primaryColumnsByTable]);

  const keyColumnsByTable = useMemo(() => {
    const keys: Record<string, string[]> = {};
    const add = (table: string, col: string) => {
      const list = keys[table] || (keys[table] = []);
      if (!list.includes(col)) list.push(col);
    };
    for (const table of Object.keys(tablesInSchema)) {
      for (const col of primaryColumnsByTable[table] || ["id"]) add(table, col);
    }
    for (const rel of relations) {
      add(rel.from, rel.fromCol);
      add(rel.to, rel.toCol);
    }
    return keys;
  }, [tablesInSchema, relations, primaryColumnsByTable]);

  const neighbourMap = useMemo(() => {
    const map: Record<string, Set<string>> = {};
    const link = (a: string, b: string) => {
      (map[a] || (map[a] = new Set())).add(b);
    };
    for (const rel of relations) {
      link(rel.from, rel.to);
      link(rel.to, rel.from);
    }
    return map;
  }, [relations]);

  // Focus mode: at 161 tables the whole graph is a hairball, so the useful view
  // is one table plus the tables it actually touches. Undirected on purpose —
  // "what references me" matters as much as "what do I reference".
  const [focusDepth, setFocusDepth] = useState(1);
  // Most of the lines around a hub are not attached to it — they run between
  // its neighbours. When the question is "what connects to this table" they are
  // noise, so edges between two tables at the same distance are off by default.
  const [showSiblingEdges, setShowSiblingEdges] = useState(false);

  // The hop distance is kept, not just the membership: it is what lets the
  // layout draw a star as a star instead of packing it into a grid.
  const { visibleTables, hopByTable, limited } = useMemo(() => {
    const window = erdTableWindow(
      tablesInSchema,
      neighbourMap,
      focusTable,
      focusDepth,
    );
    return {
      visibleTables: Object.fromEntries(
        window.names.map((name) => [name, tablesInSchema[name]]),
      ),
      hopByTable: window.hops,
      limited: window.limited,
    };
  }, [tablesInSchema, neighbourMap, focusTable, focusDepth]);

  // Collapsed is the default: 161 tables at full column height is unreadable,
  // and the columns that carry meaning in an ERD are the keys.
  const [expandedTables, setExpandedTables] = useState<Record<string, boolean>>(
    {},
  );
  const toggleTable = useCallback((table: string) => {
    setExpandedTables((current) => ({ ...current, [table]: !current[table] }));
  }, []);

  // A fixed row height overlapped as soon as one table had more columns than
  // the row allowed for. Each node's height is estimated from its own column
  // count, and nodes drop into whichever column is currently shortest, so a
  // wide table pushes only the column it lands in.
  const { nodes: computedNodes, edges: computedEdges } = useMemo(() => {
    const tableList = Object.entries(visibleTables);

    // Marching dashes repaint on every frame — stroke-dashoffset is not
    // composited — so the cost climbs with the graph. Under a focus it is a
    // handful of edges and worth the motion; across a whole schema it is not.
    const animated = tableList.length <= ANIMATED_NODE_LIMIT;
    const nodeWidth = 380;
    const HEADER = 40;
    const ROW = 22;
    const GAP = 32;

    // Heights first, positions second: a hop layout has to know how tall a
    // whole column is before it can centre it.
    const specs = tableList.map(([tableName, columns]) => {
      const keyColumns = keyColumnsByTable[tableName] || [];
      const expanded = !!expandedTables[tableName];
      const rowCount = expanded
        ? (columns?.length ?? 0)
        : (columns || []).filter((c) => keyColumns.includes(c)).length;
      return {
        tableName,
        columns: columns || [],
        keyColumns,
        expanded,
        // Matches the node's own max-height: a 32-column table scrolls inside
        // itself rather than becoming a wall five times taller than everything
        // beside it and dragging the whole column out of alignment.
        height: HEADER + Math.min(rowCount * ROW, MAX_NODE_BODY),
      };
    });

    const positions = new Map<string, { x: number; y: number }>();
    const focused = focusTable && visibleTables[focusTable];

    if (focused) {
      for (const [name, position] of placeByHop(
        specs,
        hopByTable,
        neighbourMap,
        {
          nodeWidth,
          gap: GAP,
        },
      )) {
        positions.set(name, position);
      }
    } else {
      // No focus, no centre to arrange around — pack into the shortest column.
      const colsCount = Math.max(1, Math.ceil(Math.sqrt(specs.length)));
      const columnBottoms = new Array(colsCount).fill(50);
      for (const spec of specs) {
        const colIdx = columnBottoms.indexOf(Math.min(...columnBottoms));
        positions.set(spec.tableName, {
          x: colIdx * nodeWidth + 50,
          y: columnBottoms[colIdx],
        });
        columnBottoms[colIdx] += spec.height + GAP;
      }
    }

    const flowNodes: Node[] = specs.map((spec) => {
      const { tableName, columns, keyColumns, expanded } = spec;
      const tableFKs = declaredFKs.filter((fk) => fk.table === tableName);
      const tableInferred = inferredRelationships.filter(
        (inf) => inf.fromTable === tableName,
      );

      return {
        id: tableName,
        type: "tableNode",
        position: positions.get(tableName) ?? { x: 50, y: 50 },
        data: {
          tableName,
          schema: selectedSchema,
          columns,
          foreignKeys: tableFKs,
          inferredRefs: tableInferred,
          keyColumns,
          primaryColumns: primaryColumnsByTable[tableName] || [],
          columnTypes: Object.fromEntries(
            (typedColumns[tableName] || []).map((c) => [
              c.name,
              shortType(c.dataType),
            ]),
          ),
          isJunction: junctionTables.has(tableName),
          isFocused: tableName === focusTable,
          expanded,
          onToggle: () => toggleTable(tableName),
        },
      };
    });

    const flowEdges: Edge[] = [];

    const isSiblingEdge = (a: string, b: string) =>
      !!focused && !showSiblingEdges && hopByTable[a] === hopByTable[b];

    // A nullable foreign key means the child row may exist without a parent:
    // 0..N rather than 1..N. This is the one piece of cardinality the schema
    // already tells us for free — NOT NULL is not a guess.
    const childMarker = (variant: string, table: string, column: string) => {
      const col = (typedColumns[table] || []).find((c) => c.name === column);
      const optional = col ? col.nullable : true;
      // A unique index on the key column means at most one child row per
      // parent — one-to-one. Without index metadata every relation stays
      // one-to-many, which is the safe reading.
      const one = uniqueSingleColumns.has(`${table}\u0000${column}`);
      return `url(#erd-${variant}-${one ? "one" : "crow"}-${optional ? "opt" : "req"})`;
    };

    const handles = new Map(
      flowNodes.map((node) => [
        node.id,
        new Set(visibleColumns(node.data as TableNodeData)),
      ]),
    );
    // 1. Declared foreign keys: solid.
    //
    // Neither kind carries a label any more. "employee_id → his_employees.id"
    // repeated what the row the line leaves and the node it enters already say,
    // and at this density the labels floated in open space far from their edge.
    // Orthogonal routing over bezier for the same reason: an ERD is read by
    // following lines, and swooping curves cross each other constantly.
    declaredFKs.forEach((fk, i) => {
      if (
        visibleTables[fk.table] &&
        visibleTables[fk.referencedTable] &&
        handles.get(fk.table)?.has(fk.column) &&
        handles.get(fk.referencedTable)?.has(fk.referencedColumn) &&
        !isSiblingEdge(fk.table, fk.referencedTable)
      ) {
        flowEdges.push({
          id: `fk-${fk.table}-${fk.column}-${fk.referencedTable}-${i}`,
          source: fk.table,
          target: fk.referencedTable,
          sourceHandle: `${fk.table}-${fk.column}-source`,
          targetHandle: `${fk.referencedTable}-${fk.referencedColumn}-target`,
          type: "smoothstep",
          animated,
          // React Flow's animated class dashes the line, so a declared edge
          // cannot stay solid and still move. A long dash keeps it readable as
          // the firmer of the two next to the inferred 4/4 — and once the
          // motion is off it can go back to being properly solid.
          style: {
            stroke: "var(--status-info)",
            strokeWidth: 1.5,
            strokeDasharray: animated ? "14 4" : undefined,
          },
          markerStart: childMarker("declared", fk.table, fk.column),
          markerEnd: "url(#erd-declared-one)",
        });
      }
    });

    // 2. Inferred relations: dashed, thinner and dimmer — a guess should not
    //    look like a declared constraint.
    inferredRelationships.forEach((inf, i) => {
      if (!visibleTables[inf.fromTable] || !visibleTables[inf.toTable]) return;
      if (
        !handles.get(inf.fromTable)?.has(inf.fromCol) ||
        !handles.get(inf.toTable)?.has(inf.toCol)
      )
        return;
      if (isSiblingEdge(inf.fromTable, inf.toTable)) return;
      flowEdges.push({
        id: `inf-${inf.fromTable}-${inf.fromCol}-${inf.toTable}-${i}`,
        source: inf.fromTable,
        target: inf.toTable,
        sourceHandle: `${inf.fromTable}-${inf.fromCol}-source`,
        targetHandle: `${inf.toTable}-${inf.toCol}-target`,
        type: "smoothstep",
        animated,
        style: {
          stroke: "var(--text-tertiary)",
          strokeWidth: 1,
          strokeDasharray: "4 4",
          opacity: 0.7,
        },
        markerStart: childMarker("inferred", inf.fromTable, inf.fromCol),
        markerEnd: "url(#erd-inferred-one)",
      });
    });

    return { nodes: flowNodes, edges: flowEdges.slice(0, ERD_EDGE_LIMIT) };
  }, [
    visibleTables,
    declaredFKs,
    inferredRelationships,
    selectedSchema,
    keyColumnsByTable,
    expandedTables,
    focusTable,
    hopByTable,
    neighbourMap,
    showSiblingEdges,
    toggleTable,
    typedColumns,
    uniqueSingleColumns,
    primaryColumnsByTable,
    junctionTables,
  ]);

  // React Flow is controlled: without these handlers a drag emits changes that
  // nobody applies, so the node springs back and the canvas feels frozen.
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);

  // Changing schema or focus swaps the whole set of tables on screen, so the
  // old positions mean nothing and the layout is taken as computed. Expanding a
  // node deliberately does NOT relayout: the node you clicked grows and nothing
  // else moves. Drags survive everything but a change of visible set.
  const layoutKey = `${selectedProfileId}|${selectedSchema}|${focusTable}|${focusDepth}`;
  const lastLayoutKey = useRef("");

  useLayoutEffect(() => {
    const relayout = lastLayoutKey.current !== layoutKey;
    lastLayoutKey.current = layoutKey;
    setNodes((current) => {
      if (relayout) return computedNodes;
      const placed = new Map(current.map((n) => [n.id, n.position]));
      return computedNodes.map((n) => ({
        ...n,
        position: placed.get(n.id) ?? n.position,
      }));
    });
  }, [computedNodes, layoutKey, setNodes]);

  useLayoutEffect(() => {
    setEdges(computedEdges);
  }, [computedEdges, setEdges]);

  // Hovering a table lifts its relations out of the crowd. Applied on the way
  // to React Flow rather than in the layout memo, so a hover never triggers a
  // relayout.
  const [hoveredTable, setHoveredTable] = useState("");

  const { displayNodes, displayEdges } = useMemo(() => {
    if (!hoveredTable) return { displayNodes: nodes, displayEdges: edges };
    const connected = new Set([hoveredTable]);
    for (const edge of edges) {
      if (edge.source === hoveredTable) connected.add(edge.target);
      if (edge.target === hoveredTable) connected.add(edge.source);
    }
    return {
      displayNodes: nodes.map((n) =>
        connected.has(n.id)
          ? n
          : { ...n, style: { ...n.style, opacity: 0.25 } },
      ),
      displayEdges: edges.map((e) =>
        e.source === hoveredTable || e.target === hoveredTable
          ? { ...e, zIndex: 1 }
          : { ...e, style: { ...e.style, opacity: 0.08 } },
      ),
    };
  }, [nodes, edges, hoveredTable]);

  return (
    <div className="flex-1 flex flex-col h-full overflow-hidden select-none">
      {/* ERD Control Bar — one row.
          The heading said "Entity Relationship Diagram" directly under a tab
          reading "ERD Diagram", and the legend took a second row of prose to
          repeat what the diagram shows. Both were paying canvas height for
          nothing. What is left is the two things that change what you see —
          schema and focus — and the two counts that are actually data. */}
      <div className="px-3 min-h-12 py-2 border-b border-border bg-surface-raised/30 flex flex-wrap items-center gap-2 shrink-0">
        <ControlSelect
          icon={<Database className="w-3.5 h-3.5 text-text-tertiary" />}
          value={selectedSchema}
          onChange={setSelectedSchema}
          title="Schema"
        >
          {schemas.map((s) => (
            <option key={s} value={s}>
              {s} · {Object.keys(schemaSnapshot?.namespace?.[s] || {}).length}{" "}
              tables
            </option>
          ))}
        </ControlSelect>

        {/* Focus reads as one control because the three parts are one decision:
            which table, how far out, and how much that left you with. Native
            selects so typing jumps to a table without building a combobox. */}
        <div className="flex items-center h-8 rounded-lg border border-border bg-surface-base overflow-hidden">
          <Crosshair className="ml-2.5 w-3.5 h-3.5 text-text-tertiary shrink-0" />
          <TableFocusSelect
            tables={tablesInSchema}
            value={tablesInSchema[focusTable] ? focusTable : ""}
            onChange={setFocusTable}
          />

          {tablesInSchema[focusTable] && (
            <>
              <span className="w-px h-4 bg-border shrink-0" />
              <BareSelect
                value={String(focusDepth)}
                onChange={(v) => setFocusDepth(Number(v))}
                title="How many relations out from the focused table"
              >
                <option value="1">1 hop</option>
                <option value="2">2 hops</option>
                <option value="3">3 hops</option>
              </BareSelect>
              <span className="px-2.5 h-full flex items-center border-l border-border text-code-11 text-text-tertiary shrink-0">
                {Object.keys(visibleTables).length}
              </span>
            </>
          )}
        </div>

        {tablesInSchema[focusTable] && (
          <Button
            variant={showSiblingEdges ? "secondary" : "ghost"}
            size="sm"
            className="h-8 text-label-12 gap-1.5"
            onClick={() => setShowSiblingEdges((v) => !v)}
            title="Show relations between the neighbours themselves, not just the ones touching the focused table"
          >
            <Spline className="w-3.5 h-3.5" />
            Sibling links
          </Button>
        )}

        <Button
          variant="ghost"
          size="sm"
          className="h-8 text-label-12 gap-1.5"
          onClick={() =>
            setExpandedTables((current) =>
              Object.values(current).some(Boolean)
                ? {}
                : Object.fromEntries(
                    Object.keys(visibleTables).map((t) => [t, true]),
                  ),
            )
          }
        >
          {Object.values(expandedTables).some(Boolean) ? (
            <>
              <ChevronsDownUp className="w-3.5 h-3.5" /> Collapse all
            </>
          ) : (
            <>
              <ChevronsUpDown className="w-3.5 h-3.5" /> Expand all
            </>
          )}
        </Button>

        <div className="flex-1" />

        {indexQuery.isSuccess && indexes.length === 0 && (
          <span
            className="flex items-center gap-1.5 h-7 px-2.5 rounded-lg bg-status-warning/10 text-status-warning text-caption-11"
            title="No index metadata for this schema, so every relation is drawn as one-to-many. One-to-one needs a unique index to prove it."
          >
            <AlertTriangle className="w-3 h-3" />
            no index data
          </span>
        )}

        <div className="flex items-center gap-3 text-caption-11 text-text-secondary">
          <span
            className="flex items-center gap-1.5"
            title="Foreign keys the database actually enforces"
          >
            <span className="w-4 h-px bg-status-info" />
            {declaredFKs.length} declared
          </span>
          <span
            className="flex items-center gap-1.5"
            title="Guessed from column names and types — not enforced by the database"
          >
            <span className="w-4 border-t border-dashed border-text-tertiary" />
            {inferredRelationships.length} inferred
          </span>
        </div>

        <ActionTooltip
          label={`Crow's foot = many · bar = one · circle = optional. Edges stop animating above ${ANIMATED_NODE_LIMIT} tables.`}
        >
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Relationship legend"
          >
            <Info className="size-3.5" />
          </Button>
        </ActionTooltip>
      </div>

      {metadataPending ? (
        <PendingStatus className="px-3 py-1">
          Refreshing relation metadata…
        </PendingStatus>
      ) : null}
      {metadataError ? (
        <FeedbackBanner
          tone="warning"
          title="Relation metadata incomplete"
          action={
            <Button
              variant="outline"
              size="sm"
              disabled={metadataPending}
              onClick={() => {
                void fkQuery.refetch();
                void indexQuery.refetch();
              }}
            >
              Retry
            </Button>
          }
        >
          {errorMessage(metadataError)}. Existing metadata stays visible;
          inferred links are guesses.
        </FeedbackBanner>
      ) : null}
      {limited || computedEdges.length >= ERD_EDGE_LIMIT ? (
        <FeedbackBanner title="Showing a bounded diagram">
          Up to 100 tables and 400 links are shown. Choose a focused table from
          the sidebar or focus control to explore another part of the schema.
        </FeedbackBanner>
      ) : null}
      {/* React Flow Canvas */}
      <div className="flex-1 relative">
        <ERDMarkers />
        {!schemaSnapshot ? (
          <PendingStatus className="p-6">
            Waiting for the indexed schema…
          </PendingStatus>
        ) : nodes.length > 0 ? (
          <ReactFlow
            nodes={displayNodes}
            edges={displayEdges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeMouseEnter={(_, node) => setHoveredTable(node.id)}
            onNodeMouseLeave={() => setHoveredTable("")}
            nodeTypes={nodeTypes}
            // Controls follow the resolved app theme; canvas colors retain theme tokens.
            colorMode={resolvedTheme}
            onlyRenderVisibleElements
            fitView
            fitViewOptions={{ maxZoom: 1 }}
            minZoom={0.2}
            maxZoom={2}
          >
            <FitOnFocusChange trigger={layoutKey} />
            <Background color="var(--border)" gap={20} size={1} />
            <Controls className="bg-surface-raised border-border text-text-primary fill-current" />
            <MiniMap
              className="border border-border rounded-lg overflow-hidden"
              bgColor="var(--surface-base)"
              nodeColor="var(--text-tertiary)"
              nodeStrokeColor="transparent"
              maskColor="color-mix(in srgb, var(--surface-base) 70%, transparent)"
            />
          </ReactFlow>
        ) : (
          <div className="h-full flex flex-col items-center justify-center text-center p-8 text-text-tertiary">
            <Network className="w-8 h-8 text-text-tertiary mb-2 opacity-50" />
            <h3 className="text-title-14 text-text-primary mb-1">
              No Tables in Schema &ldquo;{selectedSchema}&rdquo;
            </h3>
            <p className="text-body-12 max-w-sm text-text-secondary">
              The selected schema has no tables indexed in the local cache.
              Index your schema from the sidebar explorer.
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
