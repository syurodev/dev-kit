import { Database as DatabaseIcon, Table as TableIcon } from "lucide-react";
import { rank } from "@/workbench/palette/match";
import type { ConnectionProfile, SchemaSnapshot } from "@/bridge/database";
import type { PaletteSearchGroup } from "../registry";

const LIMIT = 8;

type TableEntry = { schema: string; table: string };

function flattenTables(snapshot: SchemaSnapshot | null): TableEntry[] {
  if (!snapshot) return [];
  const out: TableEntry[] = [];
  for (const [schema, tables] of Object.entries(snapshot.namespace)) {
    for (const table of Object.keys(tables)) out.push({ schema, table });
  }
  return out;
}

export type DatabasePaletteData = {
  schemaSnapshot: SchemaSnapshot | null;
  profiles: ConnectionProfile[];
};

export type DatabasePaletteActions = {
  openTable: (schema: string, table: string) => void;
  openConnection: (id: string) => void;
};

/**
 * The pure half: what to show for `query`, given the schema and the
 * connections already read out of the store. No table is offered for an
 * empty query — a snapshot can hold hundreds of them, and an unprompted list
 * that long is not a shortlist. Connections are, so they are always offered.
 */
export function buildGroups(
  query: string,
  data: DatabasePaletteData,
  actions: DatabasePaletteActions,
): PaletteSearchGroup[] {
  const groups: PaletteSearchGroup[] = [];
  const trimmed = query.trim();

  if (trimmed) {
    const tables = rank(
      trimmed,
      flattenTables(data.schemaSnapshot),
      (t) => ({ title: t.table, others: [`${t.schema}.${t.table}`, t.schema] }),
      LIMIT,
    );
    if (tables.length > 0) {
      groups.push({
        heading: "TABLES",
        items: tables.map((t) => ({
          id: `table:${t.schema}.${t.table}`,
          title: t.table,
          subtitle: t.schema,
          icon: TableIcon,
          run: () => actions.openTable(t.schema, t.table),
        })),
      });
    }
  }

  const connections = rank(
    trimmed,
    data.profiles,
    (p) => ({ title: p.name, others: [p.host, p.database].filter(Boolean) }),
    LIMIT,
  );
  if (connections.length > 0) {
    groups.push({
      heading: "CONNECTIONS",
      items: connections.map((p) => ({
        id: `connection:${p.id}`,
        title: p.name,
        subtitle: p.database ? `${p.host} / ${p.database}` : p.host,
        icon: DatabaseIcon,
        run: () => actions.openConnection(p.id),
      })),
    });
  }

  return groups;
}

// Asked for once, not on every keystroke: someone with no connections at all
// would otherwise have the list re-read for each letter they type.
let askedForProfiles = false;

/**
 * Database's contribution to the command palette: tables of the selected
 * connection, and the connections themselves.
 *
 * The store is cold until the sidebar mounts (databaseStore.ts), which may
 * never have happened this session — so the connections are read here too,
 * as the sidebar would.
 *
 * The schema is NOT. loadSchemaSnapshot falls back to indexing, which connects
 * to the database, and this runs on every keystroke: a search box must not
 * open connections, least of all retry an unreachable one per letter. It is
 * also never needed — selectedProfileId is not persisted, so a connection
 * being selected means this session selected it, which already started that
 * load. A snapshot still missing is loading or failed, and either way the
 * answer for now is "no tables".
 *
 * Nothing here throws: a read that fails leaves the search answering with
 * whatever it already had, which may be nothing.
 */
export async function searchPalette(query: string, signal: AbortSignal): Promise<PaletteSearchGroup[]> {
  try {
    const [{ useDatabaseStore }, { openTableTab }, { useWorkbenchTabsStore }] = await Promise.all([
      import("./databaseStore"),
      import("./documents"),
      import("@/stores/workbenchTabs"),
    ]);

    if (!askedForProfiles && useDatabaseStore.getState().profiles.length === 0) {
      askedForProfiles = true;
      await useDatabaseStore.getState().fetchProfiles();
    }
    if (signal.aborted) return [];

    const finalState = useDatabaseStore.getState();
    return buildGroups(
      query,
      { schemaSnapshot: finalState.schemaSnapshot, profiles: finalState.profiles },
      {
        openTable: (schema, table) => openTableTab(schema, table, false),
        openConnection: (id) => {
          useDatabaseStore.getState().setSelectedProfileId(id);
          useWorkbenchTabsStore.getState().focusModule("database");
        },
      },
    );
  } catch {
    return [];
  }
}
