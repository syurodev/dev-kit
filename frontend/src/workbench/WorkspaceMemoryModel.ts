import type {
  BrowseInput,
  MemoryInput,
  MemorySource,
} from "@/bridge/workspaceMemory";
export const emptyMemory = (): MemoryInput => ({
  kind: "knowledge",
  title: "",
  summary: "",
  body: "",
});
export function memoryDraft(entry: MemoryInput): MemoryInput {
  return {
    kind: entry.kind,
    title: entry.title,
    summary: entry.summary,
    body: entry.body,
  };
}
export function sameDraft(a: MemoryInput, b: MemoryInput): boolean {
  return (
    a.kind === b.kind &&
    a.title === b.title &&
    a.summary === b.summary &&
    a.body === b.body
  );
}
export function validateMemory(input: MemoryInput): string {
  for (const [name, max] of [
    ["title", 256],
    ["summary", 1024],
    ["body", 32768],
  ] as const) {
    if (!input[name].trim())
      return `${name[0].toUpperCase() + name.slice(1)} is required.`;
    if (new TextEncoder().encode(input[name]).length > max)
      return `${name[0].toUpperCase() + name.slice(1)} exceeds ${max.toLocaleString()} UTF-8 bytes.`;
  }
  return "";
}
export function sourceLabel(source: MemorySource): string {
  return [
    source.agentType || source.caller || "Unknown source",
    source.sessionId && `session ${source.sessionId}`,
  ]
    .filter(Boolean)
    .join(" · ");
}
/** Each request family owns a generation, invalidated even if transport cannot cancel. */
export class LatestRequest {
  private generation = 0;
  begin(): () => boolean {
    const current = ++this.generation;
    return () => current === this.generation;
  }
  invalidate(): void {
    this.generation++;
  }
}
/** Retain cursors only; results and bodies are never accumulated across pages. */
export function nextCursors(previous: string[], next: string): string[] {
  return next ? [...previous, next] : previous;
}

export function memoryBrowseInput(
  query: string,
  kind: string,
  deleted: boolean,
  cursor: string,
): BrowseInput {
  return { query, kind, deleted, cursor, limit: 20 };
}
export function isMemoryConflict(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    error.code === "memory.revisionConflict"
  );
}
