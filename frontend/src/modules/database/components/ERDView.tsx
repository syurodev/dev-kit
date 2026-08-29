import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
  MarkerType,
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
  Crosshair,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { useDatabaseStore } from "../databaseStore";
import {
  listForeignKeys,
  listColumns,
  type ForeignKey,
  type ColumnInfo,
} from "../../../bridge/database";

/**
 * The naming heuristic needs types, not just names. Two columns are treated as
 * linkable when they sit in the same family — an int cannot reference a uuid,
 * and matching names across families is exactly the false positive the design
 * page warns about.
 */
function typeFamily(dataType: string): string {
  const t = dataType.toLowerCase();
  if (t.includes("uuid")) return "uuid";
  if (t.includes("int") || t.includes("serial") || t.includes("numeric") || t.includes("decimal")) {
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
  expanded: boolean;
  onToggle: () => void;
};

function visibleColumns(data: TableNodeData): string[] {
  if (data.expanded) return data.columns;
  return data.columns.filter((c) => data.keyColumns.includes(c));
}

function TableNode({ data }: { data: TableNodeData }) {
  return (
    <div className="bg-surface-raised border border-border/80 rounded-xl shadow-lg min-w-[220px] max-w-[280px] overflow-hidden text-xs font-mono">
      {/* Node Header — doubles as the expand toggle */}
      <button
        type="button"
        onClick={data.onToggle}
        className="w-full px-3 py-2 bg-surface-base border-b border-border flex items-center justify-between gap-2 hover:bg-surface-raised/60 text-left"
      >
        <div className="flex items-center gap-1.5 truncate">
          {data.expanded ? (
            <ChevronDown className="w-3 h-3 text-text-tertiary shrink-0" />
          ) : (
            <ChevronRight className="w-3 h-3 text-text-tertiary shrink-0" />
          )}
          <TableIcon className="w-3.5 h-3.5 text-action-primary shrink-0" />
          <span className="font-semibold text-text-primary truncate">{data.tableName}</span>
        </div>
        <span className="text-[10px] text-text-tertiary font-sans bg-surface-raised px-1.5 py-0.5 rounded">
          {data.expanded
            ? `${data.columns.length} cols`
            : `${visibleColumns(data).length}/${data.columns.length}`}
        </span>
      </button>

      {/* Columns List */}
      <div className="p-2 space-y-1 bg-surface-raised/40">
        {visibleColumns(data).map((col) => {
          const isPK = col === "id";
          const isDeclaredFK = data.foreignKeys.some((fk) => fk.column === col);
          const isInferredFK = data.inferredRefs.some((inf) => inf.fromCol === col);

          return (
            <div
              key={col}
              className="relative flex items-center justify-between py-0.5 px-1.5 rounded hover:bg-surface-base/80 text-[11px]"
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
                  <Key className="w-3 h-3 text-amber-400 shrink-0" />
                ) : isDeclaredFK ? (
                  <LinkIcon className="w-3 h-3 text-sky-400 shrink-0" />
                ) : isInferredFK ? (
                  <LinkIcon className="w-3 h-3 text-text-tertiary shrink-0" />
                ) : (
                  <Columns className="w-3 h-3 text-text-tertiary shrink-0" />
                )}
                <span
                  className={`truncate ${
                    isPK
                      ? "text-amber-400 font-semibold"
                      : isDeclaredFK
                      ? "text-sky-400 font-medium"
                      : "text-text-primary"
                  }`}
                >
                  {col}
                </span>
              </div>

              {isPK ? (
                <span className="text-[9px] text-amber-400 bg-amber-400/10 px-1 rounded">PK</span>
              ) : isDeclaredFK ? (
                <span className="text-[9px] text-sky-400 bg-sky-400/10 px-1 rounded">FK</span>
              ) : isInferredFK ? (
                <span className="text-[9px] text-text-tertiary bg-surface-base px-1 rounded">
                  inf
                </span>
              ) : null}

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
  const hit = candidates.find((name) => name !== tableName && tableNames.includes(name));
  return hit ?? "";
}

export function ERDView() {
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
  } = useDatabaseStore();
  const [declaredFKs, setDeclaredFKs] = useState<ForeignKey[]>([]);

  const schemas = useMemo(() => {
    return Object.keys(schemaSnapshot?.namespace || {});
  }, [schemaSnapshot]);

  useEffect(() => {
    if (schemas.length > 0 && !schemas.includes(selectedSchema)) {
      setSelectedSchema(schemas[0]);
    }
  }, [schemas, selectedSchema]);

  // Load declared Foreign Keys
  useEffect(() => {
    async function loadFKs() {
      if (!selectedProfileId || !selectedSchema) return;
      try {
        const fks = await listForeignKeys(selectedProfileId, selectedSchema);
        setDeclaredFKs(fks);
      } catch {
        setDeclaredFKs([]);
      }
    }
    void loadFKs();
  }, [selectedProfileId, selectedSchema]);

  // Table names come from the cache; the ERD then fetches typed columns because
  // the naming heuristic cannot run on names alone. One pass on open — the
  // design page notes the ERD is opened far less often than the console is
  // typed in, so this is not on any hot path.
  const [typedColumns, setTypedColumns] = useState<Record<string, ColumnInfo[]>>({});

  const tablesInSchema = useMemo(() => {
    if (!schemaSnapshot?.namespace || !schemaSnapshot.namespace[selectedSchema]) {
      return {};
    }
    return schemaSnapshot.namespace[selectedSchema] || {};
  }, [schemaSnapshot, selectedSchema]);

  useEffect(() => {
    let cancelled = false;
    async function loadColumns() {
      const names = Object.keys(tablesInSchema);
      if (!selectedProfileId || !selectedSchema || names.length === 0) {
        setTypedColumns({});
        return;
      }
      const entries = await Promise.all(
        names.map(async (table) => {
          try {
            return [table, await listColumns(selectedProfileId, selectedSchema, table)] as const;
          } catch {
            return [table, [] as ColumnInfo[]] as const;
          }
        }),
      );
      if (!cancelled) setTypedColumns(Object.fromEntries(entries));
    }
    void loadColumns();
    return () => {
      cancelled = true;
    };
  }, [selectedProfileId, selectedSchema, tablesInSchema]);

  // Compute Inferred Relationships (<x>_id -> <x>.id or <x>s.id)
  const inferredRelationships = useMemo(() => {
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
          (fk) => fk.table === tableName && fk.column === col
        );
        if (isDeclared) continue;

        if (col.endsWith("_id") && col !== "id") {
          const targetTable = resolveRefTarget(tableName, col, tableNames);

          // A matching table name is not enough. The design page requires the
          // target to actually have a single-column `id` primary key whose type
          // family matches — without this the diagram draws edges to tables
          // that have no id at all.
          const sourceColumn = (typedColumns[tableName] || []).find((c) => c.name === col);
          const targetID = (typedColumns[targetTable] || []).find((c) => c.name === "id");
          const typesLineUp =
            !!sourceColumn &&
            !!targetID &&
            typeFamily(sourceColumn.dataType) === typeFamily(targetID.dataType);

          if (targetTable && targetTable !== tableName && typesLineUp) {
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
  }, [tablesInSchema, declaredFKs, typedColumns]);

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

  const keyColumnsByTable = useMemo(() => {
    const keys: Record<string, string[]> = {};
    const add = (table: string, col: string) => {
      const list = keys[table] || (keys[table] = []);
      if (!list.includes(col)) list.push(col);
    };
    for (const table of Object.keys(tablesInSchema)) add(table, "id");
    for (const rel of relations) {
      add(rel.from, rel.fromCol);
      add(rel.to, rel.toCol);
    }
    return keys;
  }, [tablesInSchema, relations]);

  // Focus mode: at 161 tables the whole graph is a hairball, so the useful view
  // is one table plus the tables it actually touches. Undirected on purpose —
  // "what references me" matters as much as "what do I reference".
  const [focusDepth, setFocusDepth] = useState(1);

  const visibleTables = useMemo(() => {
    if (!focusTable || !tablesInSchema[focusTable]) return tablesInSchema;
    const neighbours: Record<string, Set<string>> = {};
    const link = (a: string, b: string) => {
      (neighbours[a] || (neighbours[a] = new Set())).add(b);
    };
    for (const rel of relations) {
      link(rel.from, rel.to);
      link(rel.to, rel.from);
    }
    const seen = new Set([focusTable]);
    let frontier = [focusTable];
    for (let hop = 0; hop < focusDepth; hop++) {
      const next: string[] = [];
      for (const table of frontier) {
        for (const n of neighbours[table] || []) {
          if (!seen.has(n)) {
            seen.add(n);
            next.push(n);
          }
        }
      }
      frontier = next;
    }
    return Object.fromEntries(
      Object.entries(tablesInSchema).filter(([name]) => seen.has(name)),
    );
  }, [tablesInSchema, relations, focusTable, focusDepth]);

  // Collapsed is the default: 161 tables at full column height is unreadable,
  // and the columns that carry meaning in an ERD are the keys.
  const [expandedTables, setExpandedTables] = useState<Record<string, boolean>>({});
  const toggleTable = useCallback((table: string) => {
    setExpandedTables((current) => ({ ...current, [table]: !current[table] }));
  }, []);

  // A fixed row height overlapped as soon as one table had more columns than
  // the row allowed for. Each node's height is estimated from its own column
  // count, and nodes drop into whichever column is currently shortest, so a
  // wide table pushes only the column it lands in.
  const { nodes: computedNodes, edges: computedEdges } = useMemo(() => {
    const tableList = Object.entries(visibleTables);
    const colsCount = Math.max(1, Math.ceil(Math.sqrt(tableList.length)));
    const nodeWidth = 320;
    const HEADER = 40;
    const ROW = 22;
    const GAP = 32;
    const columnBottoms = new Array(colsCount).fill(50);

    const flowNodes: Node[] = tableList.map(([tableName, columns]) => {
      const keyColumns = keyColumnsByTable[tableName] || [];
      const expanded = !!expandedTables[tableName];
      const rowCount = expanded
        ? columns?.length ?? 0
        : (columns || []).filter((c) => keyColumns.includes(c)).length;

      const colIdx = columnBottoms.indexOf(Math.min(...columnBottoms));
      const y = columnBottoms[colIdx];
      columnBottoms[colIdx] = y + HEADER + rowCount * ROW + GAP;

      const tableFKs = declaredFKs.filter((fk) => fk.table === tableName);
      const tableInferred = inferredRelationships.filter(
        (inf) => inf.fromTable === tableName
      );

      return {
        id: tableName,
        type: "tableNode",
        position: { x: colIdx * nodeWidth + 50, y },
        data: {
          tableName,
          schema: selectedSchema,
          columns: columns || [],
          foreignKeys: tableFKs,
          inferredRefs: tableInferred,
          keyColumns,
          expanded,
          onToggle: () => toggleTable(tableName),
        },
      };
    });

    const flowEdges: Edge[] = [];

    // 1. Declared Foreign Keys (Solid edges)
    declaredFKs.forEach((fk, i) => {
      if (visibleTables[fk.table] && visibleTables[fk.referencedTable]) {
        flowEdges.push({
          id: `fk-${fk.table}-${fk.column}-${fk.referencedTable}-${i}`,
          source: fk.table,
          target: fk.referencedTable,
          sourceHandle: `${fk.table}-${fk.column}-source`,
          targetHandle: `${fk.referencedTable}-${fk.referencedColumn}-target`,
          animated: true,
          style: { stroke: "var(--status-info)", strokeWidth: 2 },
          markerEnd: {
            type: MarkerType.ArrowClosed,
            color: "var(--status-info)",
          },
          label: `${fk.column} → ${fk.referencedTable}.${fk.referencedColumn}`,
          labelStyle: { fill: "var(--status-info)", fontSize: 10, fontFamily: "monospace" },
          labelBgStyle: { fill: "var(--surface-base)", fillOpacity: 0.8 },
        });
      }
    });

    // 2. Inferred Relationships (Dashed edges with 'Inferred' label)
    inferredRelationships.forEach((inf, i) => {
      if (!visibleTables[inf.fromTable] || !visibleTables[inf.toTable]) return;
      flowEdges.push({
        id: `inf-${inf.fromTable}-${inf.fromCol}-${inf.toTable}-${i}`,
        source: inf.fromTable,
        target: inf.toTable,
        sourceHandle: `${inf.fromTable}-${inf.fromCol}-source`,
        targetHandle: `${inf.toTable}-${inf.toCol}-target`,
        style: { stroke: "var(--text-tertiary)", strokeWidth: 1.5, strokeDasharray: "4 4" },
        markerEnd: {
          type: MarkerType.Arrow,
          color: "var(--text-tertiary)",
        },
        label: `${inf.fromCol} ⇢ ${inf.toTable}.id (inferred)`,
        labelStyle: { fill: "var(--text-tertiary)", fontSize: 9, fontFamily: "monospace" },
        labelBgStyle: { fill: "var(--surface-raised)", fillOpacity: 0.8 },
      });
    });

    return { nodes: flowNodes, edges: flowEdges };
  }, [
    visibleTables,
    declaredFKs,
    inferredRelationships,
    selectedSchema,
    keyColumnsByTable,
    expandedTables,
    toggleTable,
  ]);

  // React Flow is controlled: without these handlers a drag emits changes that
  // nobody applies, so the node springs back and the canvas feels frozen.
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);

  // Changing schema or focus swaps the whole set of tables on screen, so the
  // old positions mean nothing and the layout is taken as computed. Expanding a
  // node deliberately does NOT relayout: the node you clicked grows and nothing
  // else moves. Drags survive everything but a change of visible set.
  const layoutKey = `${selectedSchema}|${focusTable}|${focusDepth}`;
  const lastLayoutKey = useRef("");

  useEffect(() => {
    const relayout = lastLayoutKey.current !== layoutKey;
    lastLayoutKey.current = layoutKey;
    setNodes((current) => {
      if (relayout) return computedNodes;
      const placed = new Map(current.map((n) => [n.id, n.position]));
      return computedNodes.map((n) => ({ ...n, position: placed.get(n.id) ?? n.position }));
    });
  }, [computedNodes, layoutKey, setNodes]);

  useEffect(() => {
    setEdges(computedEdges);
  }, [computedEdges, setEdges]);

  return (
    <div className="flex-1 flex flex-col h-full overflow-hidden select-none">
      {/* ERD Control Bar */}
      <div className="px-4 py-3 border-b border-border bg-surface-raised/40 flex flex-wrap items-center justify-between gap-3 shrink-0">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <Network className="w-4 h-4 text-action-primary" />
            <h2 className="text-sm font-semibold text-text-primary">Entity Relationship Diagram</h2>
          </div>

          <div className="flex items-center gap-1.5 text-xs text-text-secondary">
            <label className="font-medium text-text-tertiary">Schema:</label>
            <select
              value={selectedSchema}
              onChange={(e) => setSelectedSchema(e.target.value)}
              className="h-7 px-2 rounded-md border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-action-primary"
            >
              {schemas.map((s) => (
                <option key={s} value={s}>
                  {s} ({Object.keys(schemaSnapshot?.namespace?.[s] || {}).length} tables)
                </option>
              ))}
            </select>
          </div>

          {/* Focus: 161 tables at once is a hairball. Native select so typing
              jumps to a table without building a combobox. */}
          <div className="flex items-center gap-1.5 text-xs text-text-secondary">
            <Crosshair className="w-3.5 h-3.5 text-text-tertiary" />
            <select
              value={tablesInSchema[focusTable] ? focusTable : ""}
              onChange={(e) => setFocusTable(e.target.value)}
              className="h-7 px-2 rounded-md border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-action-primary max-w-[220px]"
            >
              <option value="">All tables ({Object.keys(tablesInSchema).length})</option>
              {Object.keys(tablesInSchema)
                .sort()
                .map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
            </select>
            {tablesInSchema[focusTable] && (
              <>
                <select
                  value={focusDepth}
                  onChange={(e) => setFocusDepth(Number(e.target.value))}
                  className="h-7 px-2 rounded-md border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-action-primary"
                >
                  <option value={1}>1 hop</option>
                  <option value={2}>2 hops</option>
                  <option value={3}>3 hops</option>
                </select>
                <span className="text-text-tertiary">
                  {Object.keys(visibleTables).length} shown
                </span>
              </>
            )}
          </div>

          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs"
            onClick={() =>
              setExpandedTables((current) =>
                Object.values(current).some(Boolean)
                  ? {}
                  : Object.fromEntries(Object.keys(visibleTables).map((t) => [t, true])),
              )
            }
          >
            {Object.values(expandedTables).some(Boolean) ? "Collapse all" : "Expand all"}
          </Button>
        </div>

        {/* Legend */}
        <div className="flex items-center gap-4 text-[11px]">
          <div className="flex items-center gap-1.5 text-text-secondary">
            <span className="w-4 h-0.5 bg-sky-400" />
            <span>Declared Foreign Keys ({declaredFKs.length})</span>
          </div>
          <div className="flex items-center gap-1.5 text-text-secondary">
            <span className="w-4 h-0.5 border-b border-dashed border-text-tertiary" />
            <span>Inferred Relations ({inferredRelationships.length})</span>
          </div>
        </div>
      </div>

      {/* React Flow Canvas */}
      <div className="flex-1 relative">
        {nodes.length > 0 ? (
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            nodeTypes={nodeTypes}
            fitView
            fitViewOptions={{ maxZoom: 1 }}
            minZoom={0.2}
            maxZoom={2}
          >
            <FitOnFocusChange trigger={layoutKey} />
            <Background color="var(--border)" gap={20} size={1} />
            <Controls className="bg-surface-raised border-border text-text-primary fill-current" />
            <MiniMap
              className="bg-surface-raised border border-border rounded-lg overflow-hidden"
              nodeColor="var(--status-info)"
              maskColor="rgba(0, 0, 0, 0.6)"
            />
          </ReactFlow>
        ) : (
          <div className="h-full flex flex-col items-center justify-center text-center p-8 text-text-tertiary">
            <Network className="w-8 h-8 text-text-tertiary mb-2 opacity-50" />
            <h3 className="text-sm font-semibold text-text-primary mb-1">
              No Tables in Schema &ldquo;{selectedSchema}&rdquo;
            </h3>
            <p className="text-xs max-w-sm text-text-secondary">
              The selected schema has no tables indexed in the local cache. Index your schema from the sidebar explorer.
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
