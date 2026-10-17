import { createElement, lazy } from "react";
import type { BrowsePage, MemoryEntry, MemorySummary } from "@/bridge/workspaceMemory";
import { parseToolOutput, registerToolResultView } from "./toolResultViews";

/**
 * ToolResultViews for `workspace.memory.*` (docs/dev-kit/tool-result-view.mdx).
 * Workspace memory is the workbench's, not a module's: no module in
 * internal/app/app.go owns it, and its manager is ./WorkspaceMemoryDialog.tsx,
 * opened from the title bar's workspace menu. So the workbench registers
 * them (./registerViews.ts, imported by ./Workbench.tsx beside the module
 * manifests), the way a module registers its own from its manifest. Shapes are checked here, synchronously, before the card
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

/**
 * `workspace.memory.get|create|update|delete|restore` → an Entry (a summary
 * plus its body). A write answers without the body unless it asked for it
 * (`includeBody`; internal/mcp/tools_workspace_memory.go), so the body the card
 * shows is the answer's when it has one, else the one the call sent — the text
 * the agent just wrote is in `rawInput`. A delete and a restore sent none, and
 * show a card without a body.
 */
export function parseMemoryEntry(rawOutput: unknown, rawInput?: unknown): MemoryEntry | null {
  const value = parseToolOutput(rawOutput);
  if (!isRecord(value)) return null;
  const sent = isRecord(rawInput) && typeof rawInput.body === "string" ? rawInput.body : "";
  const body = typeof value.body === "string" ? value.body : sent;
  const summary = parseSummary(value);
  return summary ? { ...summary, body } : null;
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

/** The tools answering one Entry: reads and every write, a soft delete and a restore included. */
const ENTRY_TOOLS = [
  "workspace.memory.get",
  "workspace.memory.create",
  "workspace.memory.update",
  "workspace.memory.delete",
  "workspace.memory.restore",
];
const PAGE_TOOLS = ["workspace.memory.list", "workspace.memory.search"];

/** Registers every memory view; returns what undoes each registration. */
export function registerMemoryResultViews(): (() => void)[] {
  const disposers: (() => void)[] = [];
  for (const tool of ENTRY_TOOLS) {
    disposers.push(
      registerToolResultView(tool, ({ rawInput, rawOutput, heading }) => {
        const entry = parseMemoryEntry(rawOutput, rawInput);
        return entry ? createElement(LazyMemoryEntryCard, { entry, heading }) : null;
      }),
    );
  }
  for (const tool of PAGE_TOOLS) {
    disposers.push(
      registerToolResultView(tool, ({ rawInput, rawOutput, heading }) => {
        const page = parseMemoryPage(rawOutput);
        if (!page) return null;
        const query = isRecord(rawInput) && typeof rawInput.query === "string" ? rawInput.query : "";
        return createElement(LazyMemoryListCard, { page, query, heading });
      }),
    );
  }
  return disposers;
}
