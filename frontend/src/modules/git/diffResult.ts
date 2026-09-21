import { parseToolOutput } from "@/workbench/toolResultViews";

/** Mirrors internal/gitparse/gitparse.go: Change — git.diff's rawOutput (a list). */
export type DiffChange = {
  path: string;
  kind: "added" | "modified" | "deleted" | "typechange";
  added: number;
  removed: number;
};

function isChangeList(value: unknown): value is DiffChange[] {
  return (
    Array.isArray(value) &&
    value.every((v) => v && typeof v === "object" && typeof (v as Record<string, unknown>).path === "string")
  );
}

/**
 * Guards git.diff's result before the Card component is imported. `rev`
 * comes from rawInput (internal/mcp/tools_git.go: gitDiffInput) — an empty
 * string means the work tree, same as the tool call itself.
 */
export function parseDiffResult(rawInput: unknown, rawOutput: unknown): { changes: DiffChange[]; rev: string } | null {
  const parsed = parseToolOutput(rawOutput);
  if (!isChangeList(parsed) || parsed.length === 0) return null;
  const rev = rawInput && typeof rawInput === "object" ? (rawInput as Record<string, unknown>).rev : "";
  return { changes: parsed, rev: typeof rev === "string" ? rev : "" };
}
