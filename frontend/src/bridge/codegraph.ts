import { Events } from "@wailsio/runtime";
import { callBridge } from "./client";
import { toAppError } from "./errors";
import { AppService } from "../../bindings/devkit/internal/bridge";
import type { Step as StepModel } from "../../bindings/devkit/internal/codegraph/models";

/**
 * What calls in — mirrors Go's EntryKind* (internal/codegraph/entrypoints.go).
 * Open-ended: a kind this build does not know still lists, under its own name.
 */
export type CodeGraphEntryKind =
  | "http"
  | "graphql"
  | "rpc"
  | "message"
  | "socket"
  | "schedule"
  | "plugin"
  | (string & {});

/**
 * Where a flow starts because something outside the code calls in — an HTTP
 * route, a Kong plugin phase, a message pattern, a cron job… Mirrors Go's
 * EntryPoint.
 */
export type CodeGraphEntryPoint = {
  routeId?: string;
  topic?: string;
  consumerGroup?: string;
  id: string;
  /** What the chip says: GET, ACCESS, EVENT, GRPC, CRON… */
  verb: string;
  /** What it answers to: a URL, a plugin's name, a pattern, a schedule. */
  target: string;
  entryNode: string;
  kind: CodeGraphEntryKind;
  /** The top-level bucket when a folder holds several services; "" when one. */
  service: string;
  /** What declares it (the grouping key), and how that reads as a heading. */
  owner: string;
  group: string;
  /** Run order within the group where it means something (Kong phases); 0 = none. */
  order: number;
};

export type CodeGraphFlowStep = {
  /** Calls below the entry point; steps are depth-first, so this rebuilds the tree. */
  depth: number;
  /** The condition the caller reaches this step under, as the source writes it. Empty when it always does. */
  guard?: string;
  label: string;
  filePath: string;
  line: number;
};

export type CodeGraphApiFlow = {
  entryPointId: string;
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
  /**
   * No analyzer reads this project: it is open for its files and has no
   * graph. Not a failure — see IndexInfo.unsupported in Go.
   */
  unsupported: boolean;
};

export async function openCodeGraphProject(path: string): Promise<CodeGraphDetectedProject> {
  return callBridge(async () => {
    const detected = await AppService.CodeGraphOpenProject(path);
    let unsupported = false;
    try {
      await AppService.CodeGraphWaitForIndexReady("");
    } catch (e) {
      // The folder opened; there is simply no index to wait for.
      if (toAppError(e).code !== "unsupported_project") throw e;
      unsupported = true;
    }
    return {
      language: detected.language,
      label: detected.label,
      modules: detected.modules ?? [],
      unsupported,
    };
  });
}

export async function listCodeGraphEntryPoints(servicePath = ""): Promise<CodeGraphEntryPoint[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphListEntryPoints(servicePath);
    return (list ?? []).map((e) => ({
      id: e.id,
      verb: e.verb,
      target: e.target,
      entryNode: e.entryNode,
      kind: e.kind || "http",
      service: e.service,
      owner: e.owner,
      group: e.group,
      order: e.order,
      routeId: e.routeId,
      topic: e.topic,
      consumerGroup: e.consumerGroup,
    }));
  });
}

export async function getCodeGraphApiFlow(entryPointId: string, servicePath = ""): Promise<CodeGraphApiFlow> {
  return callBridge(async () => {
    const flow = await AppService.CodeGraphGetApiFlow(entryPointId, servicePath);
    return {
      entryPointId: flow.entryPointId,
      steps: (flow.steps ?? []).map((s) => ({
        depth: s.depth,
        guard: s.guard,
        label: s.label,
        filePath: s.filePath,
        line: s.line,
      })),
    };
  });
}

export async function searchCodeGraphSymbol(query: string, servicePath = ""): Promise<CodeGraphSymbolMatch[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphSearchSymbol(query, servicePath);
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
  servicePath = "",
): Promise<CodeGraphDefinition[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphFindDefinitions(filePath, line, identifier, servicePath);
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
  servicePath = "",
): Promise<CodeGraphDefinition[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphFindImplementations(filePath, line, identifier, servicePath);
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
  servicePath = "",
): Promise<CodeGraphReference[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphFindReferences(filePath, line, identifier, servicePath);
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

export async function listCodeGraphFiles(servicePath = ""): Promise<CodeGraphFileEntry[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphListFiles(servicePath);
    return (list ?? []).map((f) => ({ path: f.path, isDir: f.isDir }));
  });
}

export async function readCodeGraphFile(relativePath: string, servicePath = ""): Promise<string> {
  return callBridge(() => AppService.CodeGraphReadFile(relativePath, servicePath));
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
  /** The condition the call runs under, as written (Go Edge.Guard); absent when it always runs. */
  guard?: string;
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
  servicePath = "",
): Promise<CodeGraphSubgraph> {
  const result = await callBridge(() =>
    AppService.CodeGraphGetNeighbors(rootId, depth, filter ?? {}, servicePath),
  );
  return {
    nodes: result.nodes ?? [],
    edges: result.edges ?? [],
    truncated: result.truncated ?? false,
    continuation: result.continuation,
  };
}

/** One control structure around a call (Go StepFrame). `kind` is open: render an unknown one by its text. */
export type CodeGraphStepFrame = { id: string; kind: string; text: string; exits?: boolean };

/** What an extractor recognised at a call (Go StepEffect). */
export type CodeGraphStepEffect = { kind: string; role: string; target: string };

/** One call in a method body, as the outline draws it (Go Step). */
export type CodeGraphStep = {
  line: number;
  col: number;
  text: string;
  calleeKey: string;
  /** The project node it lands on. */
  to?: string;
  /** The project node a call into a method with no body names; set only when `to` is not. */
  target?: string;
  /** Overrides of `to`, when `to` is abstract or overridden. */
  impls?: string[];
  frames: CodeGraphStepFrame[];
  visible: boolean;
  reason: string;
  effect?: CodeGraphStepEffect;
};

export type CodeGraphSteps = {
  steps: Record<string, CodeGraphStep[]>;
  truncated: string[];
  legacy: boolean;
};

/** Go's MaxStepNodeIDs: one request's limit, so longer lists go in chunks. */
export const MAX_STEP_NODE_IDS = 200;

function toStep(s: StepModel): CodeGraphStep {
  return {
    line: s.line,
    col: s.col,
    text: s.text,
    calleeKey: s.calleeKey,
    to: s.to || undefined,
    target: s.target || undefined,
    impls: s.impls && s.impls.length ? [...s.impls] : undefined,
    frames: (s.frames ?? []).flatMap((f) => (f ? [{ id: f.id, kind: f.kind, text: f.text, exits: f.exits || undefined }] : [])),
    visible: s.visible,
    reason: s.reason,
    effect: s.effect ? { kind: s.effect.kind, role: s.effect.role, target: s.effect.target } : undefined,
  };
}

/** The steps of a flow's methods, scored with the person's overrides. */
export async function getCodeGraphSteps(nodeIds: string[], servicePath = ""): Promise<CodeGraphSteps> {
  const merged: CodeGraphSteps = { steps: {}, truncated: [], legacy: false };
  for (let i = 0; i < nodeIds.length; i += MAX_STEP_NODE_IDS) {
    const chunk = nodeIds.slice(i, i + MAX_STEP_NODE_IDS);
    const result = await callBridge(() => AppService.CodeGraphGetSteps(chunk, servicePath));
    for (const [id, list] of Object.entries(result.steps ?? {})) {
      merged.steps[id] = (list ?? []).flatMap((s) => (s ? [toStep(s)] : []));
    }
    merged.truncated.push(...(result.truncated ?? []));
    merged.legacy = merged.legacy || result.legacy;
  }
  return merged;
}

export type CallVisibilityMode = "show" | "hide" | "";

/** The person's "always show / hide" choices, by callee key. */
export async function getCallVisibility(): Promise<Record<string, string>> {
  const result = await callBridge(() => AppService.PreferencesGetCallVisibility());
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(result ?? {})) if (v) out[k] = v;
  return out;
}

export async function setCallVisibility(calleeKey: string, mode: CallVisibilityMode): Promise<void> {
  await callBridge(() => AppService.PreferencesSetCallVisibility(calleeKey, mode));
}


export type CodeGraphCodeSnippet = {
  filePath: string;
  /** Boundaries actually used, which may differ from the ones requested — Go
   *  snaps a line inside a method body back to that method's real start. */
  line: number;
  endLine: number;
  source: string;
  /**
   * The Go side's label for the file ("java", "typescript", "" …). Informational:
   * the editor colours by path through components/code/languages, which knows
   * far more languages than the indexer does.
   */
  language: string;
  /** Fully-qualified name of the symbol shown; empty if it matched no single symbol. */
  fqn: string;
};

export async function getCodeGraphCodeSnippet(
  filePath: string,
  line: number,
  endLine: number,
  servicePath = "",
): Promise<CodeGraphCodeSnippet> {
  return callBridge(() => AppService.CodeGraphGetCodeSnippet(filePath, line, endLine, servicePath));
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
  servicePath = "",
): Promise<CodeGraphDataFlowTrace> {
  const trace = await callBridge(() => AppService.CodeGraphTraceDataFlow(symbolId, fieldName, servicePath));
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

export async function listCodeGraphFields(nodeId: string, servicePath = ""): Promise<CodeGraphFieldRef[]> {
  return callBridge(async () => (await AppService.CodeGraphListFields(nodeId, servicePath)) ?? []);
}

/** Every file that transitively depends on this one — what a change here can break. */
export async function getCodeGraphBlastRadius(filePath: string, servicePath = ""): Promise<string[]> {
  return callBridge(async () => (await AppService.CodeGraphGetBlastRadius(filePath, servicePath)) ?? []);
}

/** Blocks until no re-index is pending, then resolves to the index version. */
export async function waitForCodeGraphIndexReady(servicePath = ""): Promise<number> {
  return callBridge(() => AppService.CodeGraphWaitForIndexReady(servicePath));
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
  /**
   * A reopened project answering from its stored index while the refresh
   * runs — same analyzer, same project, so only what the files changed can
   * differ. Optional because the UI builds optimistic copies of this.
   */
  stale?: boolean;
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
  /**
   * No analyzer reads this project: open for its files, no graph. Shown as a
   * "not supported" label, never as an error. Optional because the UI builds
   * optimistic copies of this.
   */
  unsupported?: boolean;
};

export function codeGraphIndexInfo(servicePath = ""): Promise<CodeGraphIndexInfo> {
  return callBridge(async () => {
    const info = await AppService.CodeGraphIndexInfo(servicePath);
    return {
      indexedAt: info.indexedAt,
      indexing: info.indexing,
      stale: info.stale ?? false,
      error: info.error,
      modules: info.modules ?? [],
      language: info.language ?? "",
      branch: info.branch,
      unsupported: info.unsupported ?? false,
    };
  });
}

/** M modified, A added or untracked, D deleted, R renamed. */
export type CodeGraphGitState = "M" | "A" | "D" | "R";

/** Changed files of the active project, keyed like CodeGraphFileEntry.path. */
export function codeGraphGitStatus(servicePath = ""): Promise<Record<string, CodeGraphGitState>> {
  return callBridge(async () => {
    const map = (await AppService.CodeGraphGitStatus(servicePath)) ?? {};
    return map as Record<string, CodeGraphGitState>;
  });
}

const INDEX_UPDATED_EVENT = "codegraph:indexUpdated";

/**
 * Fires after a background re-index actually changed the index — the file
 * watcher's whole point, since it means the UI never has to poll. servicePath
 * names the folder re-indexed: the focused project's root, or a workspace
 * service open beside it. Returns an unsubscribe function, like the agenthub
 * subscriptions.
 */
export function onCodeGraphIndexUpdated(
  handler: (version: number, servicePath: string) => void,
): () => void {
  return Events.On(INDEX_UPDATED_EVENT, (ev) => {
    handler(Number(ev.data?.version ?? 0), String(ev.data?.servicePath ?? ""));
  });
}

const SERVICE_CLOSED_EVENT = "codegraph:serviceClosed";

/**
 * Fires when a workspace service kept open beside the focused project is
 * shut down to make room (codegraph.Registry's resident limit). Its next call
 * reopens it; anything cached for it is stale from here.
 */
export function onCodeGraphServiceClosed(handler: (servicePath: string) => void): () => void {
  return Events.On(SERVICE_CLOSED_EVENT, (ev) => {
    handler(String(ev.data?.servicePath ?? ""));
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
