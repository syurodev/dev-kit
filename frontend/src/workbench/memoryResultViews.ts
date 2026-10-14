import { createElement, lazy } from "react";
import type { BrowsePage, MemoryEntry, MemorySummary } from "@/bridge/workspaceMemory";
import { parseToolOutput, registerToolResultView } from "./toolResultViews";

/**
 * ToolResultViews for `workspace.memory.*` (docs/dev-kit/tool-result-view.mdx).
 * Workspace memory is the workbench's, not a module's: no module in
 * internal/app/app.go owns it, and its manager is ./WorkspaceMemoryDialog.tsx,
 * opened from the title bar's workspace menu. So the views are registered
 * from there (./WorkspaceSwitcher.tsx), the way a module registers its own
 * from its manifest. Shapes are checked here, synchronously, before the card
 * chunk loads; anything else falls back to the generic block.
 */

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function parseSummary(value: unknown): MemorySummary | null {
  if (!isRecord(value)) return null;
  const { id, kind, title, summary, revision, updatedAt } = value;
  if (typeof id !== "string" || typeof kind !== "string" || typeof title !== "string") return null;
  if (typeof summary !== "string" || typeof revision !== "number" || typeof updatedAt !== "string") return null;
  const source = isRecord(value.source) ? value.source : {};
  return {
    id,
    workspaceId: typeof value.workspaceId === "string" ? value.workspaceId : "",
    kind,
    title,
    summary,
    revision,
    deleted: value.deleted === true,
    createdAt: typeof value.createdAt === "string" ? value.createdAt : "",
    updatedAt,
    source: {
      caller: typeof source.caller === "string" ? source.caller : "",
      sessionId: typeof source.sessionId === "string" ? source.sessionId : "",
      agentType: typeof source.agentType === "string" ? source.agentType : "",
    },
  };
}

/** `workspace.memory.get|create|update` → an Entry (a summary plus its body). */
export function parseMemoryEntry(rawOutput: unknown): MemoryEntry | null {
  const value = parseToolOutput(rawOutput);
  if (!isRecord(value) || typeof value.body !== "string") return null;
  const summary = parseSummary(value);
  return summary ? { ...summary, body: value.body } : null;
}

/** `workspace.memory.list|search` → `{entries, hasMore, nextCursor?}`. */
export function parseMemoryPage(rawOutput: unknown): BrowsePage | null {
  const value = parseToolOutput(rawOutput);
  if (!isRecord(value) || !Array.isArray(value.entries)) return null;
  const entries = value.entries.map(parseSummary);
  if (!entries.every((entry): entry is MemorySummary => entry !== null)) return null;
  return {
    entries,
    hasMore: value.hasMore === true,
    nextCursor: typeof value.nextCursor === "string" ? value.nextCursor : "",
  };
}

const cards = () => import("./MemoryResultCard");
const LazyMemoryEntryCard = lazy(() => cards().then(({ MemoryEntryCard }) => ({ default: MemoryEntryCard })));
const LazyMemoryListCard = lazy(() => cards().then(({ MemoryListCard }) => ({ default: MemoryListCard })));

export function registerMemoryResultViews(): void {
  for (const tool of ["workspace.memory.get", "workspace.memory.create", "workspace.memory.update"]) {
    registerToolResultView(tool, ({ rawOutput, heading }) => {
      const entry = parseMemoryEntry(rawOutput);
      return entry ? createElement(LazyMemoryEntryCard, { entry, heading }) : null;
    });
  }
  for (const tool of ["workspace.memory.list", "workspace.memory.search"]) {
    registerToolResultView(tool, ({ rawInput, rawOutput, heading }) => {
      const page = parseMemoryPage(rawOutput);
      if (!page) return null;
      const query = isRecord(rawInput) && typeof rawInput.query === "string" ? rawInput.query : "";
      return createElement(LazyMemoryListCard, { page, query, heading });
    });
  }
}
