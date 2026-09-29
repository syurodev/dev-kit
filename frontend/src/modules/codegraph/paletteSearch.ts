import { CodeXml, FileCode2, SquareFunction, Workflow } from "lucide-react";
import { rank } from "@/workbench/palette/match";
import type { CodeGraphEntryPoint, CodeGraphFileEntry, CodeGraphSymbolMatch } from "@/bridge/codegraph";
import type { PaletteSearchGroup } from "../registry";
import { symbolDisplay } from "./sidebar/sidebarModel";

const LIMIT = 8;
/** A query shorter than this is too cheap a hit rate for a round trip to Go. */
const MIN_SYMBOL_QUERY = 2;

function fileName(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

function directoryOf(path: string): string {
  const at = path.lastIndexOf("/");
  return at === -1 ? "" : path.slice(0, at);
}

export type CodeGraphPaletteData = {
  entryPoints: CodeGraphEntryPoint[];
  files: CodeGraphFileEntry[];
  /** Only ever non-empty when the query passed MIN_SYMBOL_QUERY. */
  symbols: CodeGraphSymbolMatch[];
};

export type CodeGraphPaletteActions = {
  openEntryPoint: (entry: CodeGraphEntryPoint) => void;
  openFile: (path: string) => void;
  openSymbol: (match: CodeGraphSymbolMatch) => void;
};

/**
 * The pure half: entry points, files and symbols already fetched, turned into
 * groups for `query`. Nothing is offered for an empty query — a project's
 * file list alone can run into the thousands, and that is not a shortlist.
 */
export function buildGroups(
  query: string,
  data: CodeGraphPaletteData,
  actions: CodeGraphPaletteActions,
): PaletteSearchGroup[] {
  const trimmed = query.trim();
  if (!trimmed) return [];

  const groups: PaletteSearchGroup[] = [];

  const entryPoints = rank(
    trimmed,
    data.entryPoints,
    (ep) => ({ title: `${ep.verb} ${ep.target}`, others: [ep.target, ep.group] }),
    LIMIT,
  );
  if (entryPoints.length > 0) {
    groups.push({
      heading: "ENTRY POINTS",
      items: entryPoints.map((ep) => ({
        id: `entry:${ep.id}`,
        title: `${ep.verb} ${ep.target}`,
        icon: Workflow,
        run: () => actions.openEntryPoint(ep),
      })),
    });
  }

  const files = rank(
    trimmed,
    data.files.filter((f) => !f.isDir),
    (f) => ({ title: fileName(f.path), others: [f.path] }),
    LIMIT,
  );
  if (files.length > 0) {
    groups.push({
      heading: "FILES",
      items: files.map((f) => ({
        id: `file:${f.path}`,
        title: fileName(f.path),
        subtitle: directoryOf(f.path),
        icon: FileCode2,
        run: () => actions.openFile(f.path),
      })),
    });
  }

  const symbols = rank(
    trimmed,
    data.symbols,
    (m) => ({ title: symbolDisplay(m).name, others: [m.label] }),
    LIMIT,
  );
  if (symbols.length > 0) {
    groups.push({
      heading: "SYMBOLS",
      items: symbols.map((m) => {
        const { name, detail } = symbolDisplay(m);
        return {
          id: `symbol:${m.nodeId}`,
          title: name,
          subtitle: detail,
          icon: m.kind === "class" ? CodeXml : SquareFunction,
          run: () => actions.openSymbol(m),
        };
      }),
    });
  }

  return groups;
}

/**
 * Code Graph's contribution to the command palette: entry points, files and
 * symbols of the project in front.
 *
 * `ensureStarted` wires the module up from wherever it has not run yet this
 * session, but its own project restore is asynchronous and can be as slow as
 * a re-index — so this checks `projectPath` right after calling it rather
 * than waiting on that: a project restored from a previous mount is already
 * there, and one that never opened this session reads as "no project" rather
 * than blocking the palette on an index that may take minutes.
 */
export async function searchPalette(query: string, signal: AbortSignal): Promise<PaletteSearchGroup[]> {
  const trimmed = query.trim();
  if (!trimmed) return [];

  try {
    const [actions, { useCodeGraphStore }, bridge] = await Promise.all([
      import("./codegraphActions"),
      import("./codegraphStore"),
      import("@/bridge/codegraph"),
    ]);
    actions.ensureStarted();
    if (signal.aborted) return [];

    const state = useCodeGraphStore.getState();
    if (!state.projectPath) return [];

    let entryPoints = state.entryPoints;
    if (entryPoints.length === 0) {
      try {
        entryPoints = await bridge.listCodeGraphEntryPoints();
      } catch {
        entryPoints = [];
      }
      if (signal.aborted) return [];
    }

    let files = state.files;
    if (files.length === 0) {
      try {
        files = await bridge.listCodeGraphFiles();
      } catch {
        files = [];
      }
      if (signal.aborted) return [];
    }

    let symbols: CodeGraphSymbolMatch[] = [];
    if (trimmed.length >= MIN_SYMBOL_QUERY) {
      try {
        symbols = await bridge.searchCodeGraphSymbol(trimmed);
      } catch {
        symbols = [];
      }
      if (signal.aborted) return [];
    }

    return buildGroups(
      query,
      { entryPoints, files, symbols },
      {
        openEntryPoint: (ep) => actions.viewFlow(ep, { preview: false }),
        openFile: (path) => actions.openFile(path, null, { preview: false }),
        openSymbol: (match) => actions.viewSymbol(match, { preview: false }),
      },
    );
  } catch {
    return [];
  }
}
