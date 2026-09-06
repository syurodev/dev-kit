import { Events } from "@wailsio/runtime";
import { callBridge } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type CodeGraphEndpoint = {
  id: string;
  httpMethod: string;
  path: string;
  entryNode: string;
};

export type CodeGraphFlowStep = {
  label: string;
  filePath: string;
  line: number;
};

export type CodeGraphApiFlow = {
  endpointId: string;
  steps: CodeGraphFlowStep[];
};

export type CodeGraphSymbolMatch = {
  nodeId: string;
  label: string;
  kind: string;
  filePath: string;
  line: number;
};

export async function openCodeGraphProject(path: string): Promise<void> {
  return callBridge(() => AppService.CodeGraphOpenProject(path));
}

export async function listCodeGraphEndpoints(): Promise<CodeGraphEndpoint[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphListEndpoints();
    return (list ?? []).map((e) => ({
      id: e.id,
      httpMethod: e.httpMethod,
      path: e.path,
      entryNode: e.entryNode,
    }));
  });
}

export async function getCodeGraphApiFlow(endpointId: string): Promise<CodeGraphApiFlow> {
  return callBridge(async () => {
    const flow = await AppService.CodeGraphGetApiFlow(endpointId);
    return {
      endpointId: flow.endpointId,
      steps: (flow.steps ?? []).map((s) => ({
        label: s.label,
        filePath: s.filePath,
        line: s.line,
      })),
    };
  });
}

export async function searchCodeGraphSymbol(query: string): Promise<CodeGraphSymbolMatch[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphSearchSymbol(query);
    return (list ?? []).map((s) => ({
      nodeId: s.nodeId,
      label: s.label,
      kind: s.kind,
      filePath: s.filePath,
      line: s.line,
    }));
  });
}

export type CodeGraphFileEntry = {
  path: string;
  isDir: boolean;
};

export async function listCodeGraphFiles(): Promise<CodeGraphFileEntry[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphListFiles();
    return (list ?? []).map((f) => ({ path: f.path, isDir: f.isDir }));
  });
}

export async function readCodeGraphFile(relativePath: string): Promise<string> {
  return callBridge(() => AppService.CodeGraphReadFile(relativePath));
}

export async function writeCodeGraphFile(relativePath: string, content: string): Promise<void> {
  return callBridge(() => AppService.CodeGraphWriteFile(relativePath, content));
}

export type CodeGraphNode = {
  id: string;
  label: string;
  kind: string;
  layer: string;
  filePath: string;
  line: number;
  endLine: number;
  summary: string;
};

export type CodeGraphEdge = {
  from: string;
  to: string;
  kind: string;
};

export type CodeGraphSubgraph = {
  nodes: CodeGraphNode[];
  edges: CodeGraphEdge[];
  truncated: boolean;
  continuation?: string;
};

export async function getCodeGraphNeighbors(
  rootId: string,
  depth: number,
  filter?: { layers?: string[]; continuation?: string },
): Promise<CodeGraphSubgraph> {
  const result = await callBridge(() =>
    AppService.CodeGraphGetNeighbors(rootId, depth, filter ?? {}),
  );
  return {
    nodes: result.nodes ?? [],
    edges: result.edges ?? [],
    truncated: result.truncated ?? false,
    continuation: result.continuation,
  };
}


export type CodeGraphCodeSnippet = {
  filePath: string;
  /** Boundaries actually used, which may differ from the ones requested — Go
   *  snaps a line inside a method body back to that method's real start. */
  line: number;
  endLine: number;
  source: string;
  /** "java" | "json" | "yaml" | "markdown" | "" — drives the editor's language extension. */
  language: string;
  /** Fully-qualified name of the symbol shown; empty if it matched no single symbol. */
  fqn: string;
};

export async function getCodeGraphCodeSnippet(
  filePath: string,
  line: number,
  endLine: number,
): Promise<CodeGraphCodeSnippet> {
  return callBridge(() => AppService.CodeGraphGetCodeSnippet(filePath, line, endLine));
}

/** One event in a data-flow trace: a field read, a field write, or a call the
 *  flow continues into. Flat, not nested — `depth` is the indent level. */
export type CodeGraphDataFlowStep = {
  nodeId: string;
  label: string;
  filePath: string;
  line: number;
  kind: "read" | "write" | "call";
  depth: number;
  /** The callee, for kind === "call". Empty otherwise. */
  targetId: string;
  /** Empty when nothing was lost; otherwise the wall the trace hit. */
  unresolved: "" | "library" | "interface" | "unresolved" | "depth" | "cycle";
};

export type CodeGraphDataFlowTrace = {
  symbolId: string;
  fieldName: string;
  /** Always "source" — line order within a method, NOT execution order. */
  orderedBy: string;
  steps: CodeGraphDataFlowStep[];
};

export async function traceCodeGraphDataFlow(
  symbolId: string,
  fieldName: string,
): Promise<CodeGraphDataFlowTrace> {
  const trace = await callBridge(() => AppService.CodeGraphTraceDataFlow(symbolId, fieldName));
  return {
    symbolId: trace.symbolId,
    fieldName: trace.fieldName,
    orderedBy: trace.orderedBy,
    steps: (trace.steps ?? []) as CodeGraphDataFlowStep[],
  };
}

/** One field a node touches. `classId` is what TraceDataFlow calls symbolId. */
export type CodeGraphFieldRef = {
  classId: string;
  fieldName: string;
  reads: number;
  writes: number;
};

export async function listCodeGraphFields(nodeId: string): Promise<CodeGraphFieldRef[]> {
  return callBridge(async () => (await AppService.CodeGraphListFields(nodeId)) ?? []);
}

/** Every file that transitively depends on this one — what a change here can break. */
export async function getCodeGraphBlastRadius(filePath: string): Promise<string[]> {
  return callBridge(async () => (await AppService.CodeGraphGetBlastRadius(filePath)) ?? []);
}

/** Blocks until no re-index is pending, then resolves to the index version. */
export async function waitForCodeGraphIndexReady(): Promise<number> {
  return callBridge(() => AppService.CodeGraphWaitForIndexReady());
}

/**
 * The directory the Code Graph project was opened from, or "" when none is
 * open. Other modules use it as a default — the Agent Hub's new-session dialog
 * offers it, on the grounds that the folder you are reading is usually the
 * folder you want an agent to work in.
 */
export function codeGraphProjectRoot(): Promise<string> {
  return callBridge(
    () => AppService.CodeGraphProjectRoot() as Promise<string>,
  );
}

const INDEX_UPDATED_EVENT = "codegraph:indexUpdated";

/**
 * Fires after a background re-index actually changed the index — the file
 * watcher's whole point, since it means the UI never has to poll. Returns an
 * unsubscribe function, like the agenthub subscriptions.
 */
export function onCodeGraphIndexUpdated(handler: (version: number) => void): () => void {
  return Events.On(INDEX_UPDATED_EVENT, (ev) => {
    handler(Number(ev.data?.version ?? 0));
  });
}
