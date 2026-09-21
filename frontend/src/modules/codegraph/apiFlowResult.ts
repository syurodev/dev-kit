import { parseToolOutput } from "@/workbench/toolResultViews";

/** Mirrors internal/codegraph/codegraph.go: FlowStep. */
export type ApiFlowStep = {
  label: string;
  filePath: string;
  line: number;
  summary?: string;
};

/** Mirrors internal/codegraph/codegraph.go: ApiFlow — codegraph.getApiFlow's rawOutput. */
export type ApiFlow = {
  endpointId: string;
  steps: ApiFlowStep[];
};

function isApiFlow(value: unknown): value is ApiFlow {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return typeof v.endpointId === "string" && Array.isArray(v.steps);
}

/**
 * Guards codegraph.getApiFlow's result before any Card component is even
 * imported — kept pure and React-free so the check costs nothing in the
 * manifest's initial bundle (docs/dev-kit/tool-result-view.mdx rule 1: no
 * renderer, or one with nothing to show, falls back to JSON rather than
 * rendering something guessed).
 */
export function parseApiFlow(rawOutput: unknown): { flow: ApiFlow; endpointId: string } | null {
  const parsed = parseToolOutput(rawOutput);
  if (!isApiFlow(parsed) || parsed.steps.length === 0) return null;
  return { flow: parsed, endpointId: parsed.endpointId };
}
