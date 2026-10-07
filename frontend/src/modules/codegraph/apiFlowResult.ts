import { parseToolOutput } from "@/workbench/toolResultViews";

/** Mirrors internal/codegraph/codegraph.go: FlowStep. */
export type ApiFlowStep = {
  /** Calls below the entry point; steps are depth-first, so this rebuilds the tree. Absent in older transcripts. */
  depth?: number;
  /** The condition the caller reaches this step under, as written in the source; empty or absent when it always does. */
  guard?: string;
  label: string;
  filePath: string;
  line: number;
  summary?: string;
};

/** Mirrors internal/codegraph/codegraph.go: ApiFlow — codegraph.getApiFlow's rawOutput. */
export type ApiFlow = {
  entryPointId: string;
  steps: ApiFlowStep[];
};

/**
 * The flow, whichever way it was written: `entryPointId` today, `endpointId`
 * in a transcript from before entry points had their name.
 */
function toApiFlow(value: unknown): ApiFlow | null {
  if (!value || typeof value !== "object") return null;
  const v = value as Record<string, unknown>;
  const id = v.entryPointId ?? v.endpointId;
  if (typeof id !== "string" || !Array.isArray(v.steps)) return null;
  return { entryPointId: id, steps: v.steps as ApiFlowStep[] };
}

/**
 * Guards codegraph.getApiFlow's result before any Card component is even
 * imported — kept pure and React-free so the check costs nothing in the
 * manifest's initial bundle (docs/dev-kit/tool-result-view.mdx rule 1: no
 * renderer, or one with nothing to show, falls back to JSON rather than
 * rendering something guessed).
 */
export function parseApiFlow(rawOutput: unknown): { flow: ApiFlow; entryPointId: string } | null {
  const flow = toApiFlow(parseToolOutput(rawOutput));
  if (!flow || flow.steps.length === 0) return null;
  return { flow, entryPointId: flow.entryPointId };
}
