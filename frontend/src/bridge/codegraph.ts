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
  /** controller | service | repository | unknown. */
  layer: string;
  filePath: string;
  line: number;
};

/** What a folder turned out to hold, once opened. */
export type CodeGraphDetectedProject = {
  language: string;
  label: string;
  /**
   * Module roots relative to the opened folder, "." meaning the folder itself.
   * A repository of microservices has one entry per service.
   */
  modules: string[];
};

export async function openCodeGraphProject(path: string): Promise<CodeGraphDetectedProject> {
  return callBridge(async () => {
    const detected = await AppService.CodeGraphOpenProject(path);
    await AppService.CodeGraphWaitForIndexReady();
    return {
      language: detected.language,
      label: detected.label,
      modules: detected.modules ?? [],
    };
  });
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
      layer: s.layer,
      filePath: s.filePath,
      line: s.line,
    }));
  });
}

/**
 * One place the symbol under the cursor may be defined — mirrors Go's
 * Definition (internal/codegraph/definitions.go). `filePath` is
 * project-relative, the same form the Files tab already uses, and `label` is
 * the node's fully-qualified label ("com.acme.OrderService.create"), not
 * shortened for display — that is the caller's job.
 */
export type CodeGraphDefinition = {
  nodeId: string;
  label: string;
  kind: string;
  filePath: string;
  line: number;
};

/**
 * Go-to-definition for the identifier under the cursor at filePath:line. Not
 * found is a normal empty result, not an error (see FindDefinitions' doc
 * comment) — several results is also normal, an ambiguous overload or an
 * unresolved fallback match.
 */
export async function findCodeGraphDefinitions(
  filePath: string,
  line: number,
  identifier: string,
): Promise<CodeGraphDefinition[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphFindDefinitions(filePath, line, identifier);
    return (list ?? []).map((d) => ({
      nodeId: d.nodeId,
      label: d.label,
      kind: d.kind,
      filePath: d.filePath,
      line: d.line,
    }));
  });
}

/**
 * Go-to-implementation for the identifier at filePath:line: for a method, the
 * project methods implementing it; for a class or interface, the classes that
 * do. Asked on something that is itself an implementation, the answer is
 * empty — never the thing itself (see FindImplementations).
 */
export async function findCodeGraphImplementations(
  filePath: string,
  line: number,
  identifier: string,
): Promise<CodeGraphDefinition[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphFindImplementations(filePath, line, identifier);
    return (list ?? []).map((d) => ({
      nodeId: d.nodeId,
      label: d.label,
      kind: d.kind,
      filePath: d.filePath,
      line: d.line,
    }));
  });
}

/**
 * One place a symbol is used — mirrors Go's Reference
 * (internal/codegraph/references.go). Unlike a definition, `line` is the line
 * of the use itself, `label` names the method or class the use sits in, and
 * `code` is that source line, trimmed.
 */
export type CodeGraphReference = {
  nodeId: string;
  label: string;
  /** "call" | "read" | "write" | "inject" */
  kind: string;
  filePath: string;
  line: number;
  code: string;
};

/** Every use of the identifier at filePath:line, sorted by file then line, capped by Go. */
export async function findCodeGraphReferences(
  filePath: string,
  line: number,
  identifier: string,
): Promise<CodeGraphReference[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphFindReferences(filePath, line, identifier);
    return (list ?? []).map((r) => ({
      nodeId: r.nodeId,
      label: r.label,
      kind: r.kind,
      filePath: r.filePath,
      line: r.line,
      code: r.code,
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

/**
 * Saves a file of the open project. `expectedRoot` is the project root the
 * file was opened under: Go refuses the write (an error naming
 * "codegraph.projectChanged") when the active project has moved since, so a
 * buffer from project A can never land at the same relative path in project B.
 */
export async function writeCodeGraphFile(
  relativePath: string,
  content: string,
  expectedRoot: string,
): Promise<void> {
  return callBridge(() => AppService.CodeGraphWriteFile(relativePath, content, expectedRoot));
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
  unresolved: "" | "library" | "interface" | "unresolved" | "lombok" | "depth" | "cycle" | "repeat" | "limit";
  /** The source line, trimmed. Empty when the file could not be read. */
  code: string;
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

/** The sidebar's line under the project name (Go codegraph.IndexInfo). */
export type CodeGraphIndexInfo = {
  /** When the stored index was last replaced, Unix ms; 0 when unknown. */
  indexedAt: number;
  indexing: boolean;
  /** The last run's failure with the analyzer's output tail, or "". */
  error: string;
  modules: string[];
  /**
   * What detection decided the project is written in ("java", "lua", "" when
   * nothing was detected). A restored session learns it here, since the open
   * that decided it happened in a previous run.
   */
  language: string;
  /** The checked-out git branch, "" outside a work tree. */
  branch: string;
};

export function codeGraphIndexInfo(): Promise<CodeGraphIndexInfo> {
  return callBridge(async () => {
    const info = await AppService.CodeGraphIndexInfo();
    return {
      indexedAt: info.indexedAt,
      indexing: info.indexing,
      error: info.error,
      modules: info.modules ?? [],
      language: info.language ?? "",
      branch: info.branch,
    };
  });
}

/** M modified, A added or untracked, D deleted, R renamed. */
export type CodeGraphGitState = "M" | "A" | "D" | "R";

/** Changed files of the active project, keyed like CodeGraphFileEntry.path. */
export function codeGraphGitStatus(): Promise<Record<string, CodeGraphGitState>> {
  return callBridge(async () => {
    const map = (await AppService.CodeGraphGitStatus()) ?? {};
    return map as Record<string, CodeGraphGitState>;
  });
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

const PROJECT_CHANGED_EVENT = "codegraph:projectChanged";

/**
 * Fires whenever the active project changes — from this window's picker, or
 * from an agent switching it over MCP, which the view otherwise has no way to
 * notice. Returns an unsubscribe function.
 */
export function onCodeGraphProjectChanged(handler: (root: string) => void): () => void {
  return Events.On(PROJECT_CHANGED_EVENT, (ev) => {
    handler(String(ev.data?.root ?? ""));
  });
}

/** One way to install a missing program, already narrowed to this platform. */
export type CodeGraphInstallCommand = {
  label: string;
  command: string;
};

/** One program an analyzer build needs, and what was found on this machine. */
export type CodeGraphRequirement = {
  program: string;
  label: string;
  minVersion: string;
  /** Detected version, or "" when nothing usable was found. */
  found: string;
  /** The executable that will actually be run. */
  path: string;
  /** Where it came from — "PATH", "JAVA_HOME", "IDE-bundled Maven". */
  source: string;
  met: boolean;
  problem: string;
  installHint: string;
  install: CodeGraphInstallCommand[];
};

/** Whether one language's analyzer is built, and what stands in the way. */
export type CodeGraphToolchainStatus = {
  language: string;
  label: string;
  /** The analyzer can run now: the artifact exists AND so does its runtime. */
  ready: boolean;
  /** Separates "nothing built yet" from "built but no runtime to launch it". */
  hasArtifact: boolean;
  buildable: boolean;
  artifact: string;
  buildCommand: string;
  requirements: CodeGraphRequirement[];
};

/**
 * Per language: is that analyzer built, and if not, what is missing.
 *
 * Probes the machine for a JDK and Maven, so it costs a few subprocesses —
 * call it when the setup panel is on screen, not on a timer.
 */
export async function codeGraphAnalyzerStatus(): Promise<CodeGraphToolchainStatus[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphAnalyzerStatus();
    return (list ?? []).map((s) => ({
      language: s.language,
      label: s.label,
      ready: s.ready,
      hasArtifact: s.hasArtifact,
      buildable: s.buildable,
      artifact: s.artifact,
      buildCommand: s.buildCommand,
      requirements: (s.requirements ?? []).map((r) => ({
        program: r.program,
        label: r.label,
        minVersion: r.minVersion,
        found: r.found,
        path: r.path,
        source: r.source,
        met: r.met,
        problem: r.problem,
        installHint: r.installHint,
        install: (r.install ?? []).map((c) => ({ label: c.label, command: c.command })),
      })),
    }));
  });
}

/**
 * Builds one language's analyzer and installs it — no restart, the next index
 * uses it. Resolves when the build finishes; watch
 * onCodeGraphAnalyzerBuildOutput for what it is doing in the meantime.
 */
export async function buildCodeGraphAnalyzer(language: string): Promise<void> {
  return callBridge(() => AppService.CodeGraphBuildAnalyzer(language));
}

/**
 * Stops the analyzer build in flight, Maven and all. The build's own promise
 * then rejects with operation_cancelled. A no-op when nothing is building.
 */
export async function cancelCodeGraphAnalyzerBuild(): Promise<void> {
  return callBridge(() => AppService.CodeGraphCancelAnalyzerBuild());
}

const ANALYZER_BUILD_OUTPUT_EVENT = "codegraph:analyzerBuildOutput";

/**
 * Every line the analyzer build writes, as it writes it. A cold Maven cache
 * takes minutes, and a spinner that cannot say why is indistinguishable from a
 * hang. Returns an unsubscribe function.
 */
export function onCodeGraphAnalyzerBuildOutput(
  handler: (language: string, line: string) => void,
): () => void {
  return Events.On(ANALYZER_BUILD_OUTPUT_EVENT, (ev) => {
    handler(String(ev.data?.language ?? ""), String(ev.data?.line ?? ""));
  });
}
