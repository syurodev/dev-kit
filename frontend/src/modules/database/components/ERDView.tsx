import { useEffect, useMemo, useState } from "react";
import {
  ReactFlow,
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
} from "lucide-react";
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
/**
 * xyflow styles edges through inline SVG attributes, which cannot read Tailwind
 * classes — so the tokens are resolved from CSS custom properties at render
 * time instead of hardcoding hex. That keeps the diagram correct in both
 * themes, which literal colours never would.
 */
function token(name: string, fallback: string): string {
  if (typeof window === "undefined") return fallback;
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return value || fallback;
}

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
};

function TableNode({ data }: { data: TableNodeData }) {
  return (
    <div className="bg-surface-raised border border-border/80 rounded-xl shadow-lg min-w-[220px] max-w-[280px] overflow-hidden text-xs font-mono">
      {/* Node Header */}
      <div className="px-3 py-2 bg-surface-base border-b border-border flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5 truncate">
          <TableIcon className="w-3.5 h-3.5 text-action-primary shrink-0" />
          <span className="font-semibold text-text-primary truncate">{data.tableName}</span>
        </div>
        <span className="text-[10px] text-text-tertiary font-sans bg-surface-raised px-1.5 py-0.5 rounded">
          {data.columns.length} cols
        </span>
      </div>

      {/* Columns List */}
      <div className="p-2 space-y-1 bg-surface-raised/40">
        {data.columns.map((col) => {
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

export function ERDView() {
  const { selectedProfileId, schemaSnapshot } = useDatabaseStore();
  const [selectedSchema, setSelectedSchema] = useState("public");
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
          const stem = col.slice(0, -3);
          const pluralStem = `${stem}s`;

          let targetTable = "";
          if (tableNames.includes(stem)) {
            targetTable = stem;
          } else if (tableNames.includes(pluralStem)) {
            targetTable = pluralStem;
          }

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

  // Build React Flow nodes & edges with auto-grid positioning
  const { nodes, edges } = useMemo(() => {
    const tableList = Object.entries(tablesInSchema);
    const colsCount = Math.max(1, Math.ceil(Math.sqrt(tableList.length)));
    const nodeWidth = 320;
    const nodeHeight = 300;

    const flowNodes: Node[] = tableList.map(([tableName, columns], index) => {
      const colIdx = index % colsCount;
      const rowIdx = Math.floor(index / colsCount);

      const tableFKs = declaredFKs.filter((fk) => fk.table === tableName);
      const tableInferred = inferredRelationships.filter(
        (inf) => inf.fromTable === tableName
      );

      return {
        id: tableName,
        type: "tableNode",
        position: { x: colIdx * nodeWidth + 50, y: rowIdx * nodeHeight + 50 },
        data: {
          tableName,
          schema: selectedSchema,
          columns: columns || [],
          foreignKeys: tableFKs,
          inferredRefs: tableInferred,
        },
      };
    });

    const flowEdges: Edge[] = [];

    // 1. Declared Foreign Keys (Solid edges)
    declaredFKs.forEach((fk, i) => {
      if (tablesInSchema[fk.table] && tablesInSchema[fk.referencedTable]) {
        flowEdges.push({
          id: `fk-${fk.table}-${fk.column}-${fk.referencedTable}-${i}`,
          source: fk.table,
          target: fk.referencedTable,
          sourceHandle: `${fk.table}-${fk.column}-source`,
          targetHandle: `${fk.referencedTable}-${fk.referencedColumn}-target`,
          animated: true,
          style: { stroke: token("--status-info", "#7fb5e8"), strokeWidth: 2 },
          markerEnd: {
            type: MarkerType.ArrowClosed,
            color: token("--status-info", "#7fb5e8"),
          },
          label: `${fk.column} → ${fk.referencedTable}.${fk.referencedColumn}`,
          labelStyle: { fill: token("--status-info", "#7fb5e8"), fontSize: 10, fontFamily: "monospace" },
          labelBgStyle: { fill: token("--surface-base", "#111111"), fillOpacity: 0.8 },
        });
      }
    });

    // 2. Inferred Relationships (Dashed edges with 'Inferred' label)
    inferredRelationships.forEach((inf, i) => {
      flowEdges.push({
        id: `inf-${inf.fromTable}-${inf.fromCol}-${inf.toTable}-${i}`,
        source: inf.fromTable,
        target: inf.toTable,
        sourceHandle: `${inf.fromTable}-${inf.fromCol}-source`,
        targetHandle: `${inf.toTable}-${inf.toCol}-target`,
        style: { stroke: token("--text-tertiary", "#9a9da3"), strokeWidth: 1.5, strokeDasharray: "4 4" },
        markerEnd: {
          type: MarkerType.Arrow,
          color: token("--text-tertiary", "#9a9da3"),
        },
        label: `${inf.fromCol} ⇢ ${inf.toTable}.id (inferred)`,
        labelStyle: { fill: token("--text-tertiary", "#9a9da3"), fontSize: 9, fontFamily: "monospace" },
        labelBgStyle: { fill: token("--surface-raised", "#1d1d1d"), fillOpacity: 0.8 },
      });
    });

    return { nodes: flowNodes, edges: flowEdges };
  }, [tablesInSchema, declaredFKs, inferredRelationships, selectedSchema]);

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
            nodeTypes={nodeTypes}
            fitView
            minZoom={0.2}
            maxZoom={2}
          >
            <Background color="var(--border)" gap={20} size={1} />
            <Controls className="bg-surface-raised border-border text-text-primary fill-current" />
            <MiniMap
              className="bg-surface-raised border border-border rounded-lg overflow-hidden"
              nodeColor={token("--status-info", "#7fb5e8")}
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
