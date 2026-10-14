import type { CheckpointIndex } from "../checkpoints";
import type { PlanEntry, TranscriptItem } from "../types";
import { CheckpointStatus } from "../../../../bindings/devkit/internal/agenthub/models";

/** The agent's most recent plan in the session, or none. A subagent's plan is its own, not the session's. */
export function latestPlan(items: readonly TranscriptItem[]): PlanEntry[] {
  for (let i = items.length - 1; i >= 0; i -= 1) {
    const it = items[i];
    if (it.kind === "plan" && it.plan && !it.origin) return it.plan;
  }
  return [];
}

export type SessionChange = { path: string; added: number; removed: number; binary: boolean; checkpointId: string };

/**
 * Every file the session touched, its lines summed over turns; the diff opens
 * from the newest checkpoint that touched it. Newest-touched first, a turn's
 * own order kept inside it. Only settled checkpoints count, as in a turn's
 * header (`changeSummary`): a pending or failed one says nothing yet.
 */
export function sessionChanges(index: CheckpointIndex): SessionChange[] {
  const records = [...index.values()]
    .filter((r) => r.status === CheckpointStatus.CheckpointDone && r.changed?.length)
    .sort((a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt));
  const byPath = new Map<string, SessionChange & { turn: number; pos: number }>();
  records.forEach((record, turn) => {
    (record.changed ?? []).forEach((file, pos) => {
      const prev = byPath.get(file.path);
      byPath.set(file.path, {
        path: file.path,
        added: (prev?.added ?? 0) + file.added,
        removed: (prev?.removed ?? 0) + file.removed,
        binary: Boolean(prev?.binary || file.binary),
        checkpointId: record.id,
        turn,
        pos,
      });
    });
  });
  return [...byPath.values()]
    .sort((a, b) => b.turn - a.turn || a.pos - b.pos)
    .map(({ path, added, removed, binary, checkpointId }) => ({ path, added, removed, binary, checkpointId }));
}

const DATA_MODULES: Record<string, string> = { db: "Database", kafka: "Kafka", ssh: "SSH" };
const WIRE = "mcp__devkit__";

const CODEX_TITLE = "devkit.";

function devkitPrefix(name: string): string | null {
  const plain = name.startsWith(WIRE) ? name.slice(WIRE.length).split("_")[0] : name.split(".")[0];
  return plain in DATA_MODULES ? plain : null;
}

/**
 * Codex's MCP calls carry no tool name: the title is `server.tool`
 * (codex_translate.go), so a DevKit one reads `devkit.db.query`.
 */
function toolName(call: NonNullable<TranscriptItem["toolCall"]>): string | null {
  if (call.name) return call.name;
  if (call.title?.startsWith(CODEX_TITLE)) return call.title.slice(CODEX_TITLE.length).split("_")[0];
  return null;
}

export type ToolSource = { module: string; label: string; count: number };

/** DevKit data tools the agent called, counted per module. `module` is the module manifest id. */
export function toolSources(items: readonly TranscriptItem[]): ToolSource[] {
  const counts = new Map<string, number>();
  for (const it of items) {
    const name = it.toolCall ? toolName(it.toolCall) : null;
    if (!name) continue;
    const prefix = devkitPrefix(name);
    if (prefix) counts.set(prefix, (counts.get(prefix) ?? 0) + 1);
  }
  return [...counts].map(([prefix, count]) => ({
    module: prefix === "db" ? "database" : prefix,
    label: DATA_MODULES[prefix],
    count,
  }));
}

const URL_RE = /https?:\/\/[^\s<>()"'`]+/g;
const TRAILING_PUNCTUATION = /[.,;:!?]+$/;

export type LinkHost = { host: string; url: string };

function linkOf(raw: string): LinkHost | null {
  const trimmed = raw.replace(TRAILING_PUNCTUATION, "");
  try {
    const u = new URL(trimmed);
    if ((u.protocol !== "http:" && u.protocol !== "https:") || !u.host) return null;
    return { host: u.host, url: trimmed };
  } catch {
    return null;
  }
}

/** One item's links. Items are not changed once echoed, so each is read once. */
const linksByItem = new WeakMap<TranscriptItem, LinkHost[]>();

function itemLinks(it: TranscriptItem): LinkHost[] {
  const cached = linksByItem.get(it);
  if (cached) return cached;
  const out: LinkHost[] = [];
  const push = (raw: string) => {
    const link = linkOf(raw);
    if (link) out.push(link);
  };
  // The person's own words: a subagent's echoed brief is the agent talking.
  if (it.kind === "user_message_chunk" && !it.origin && typeof it.text === "string") {
    for (const m of it.text.matchAll(URL_RE)) push(m[0]);
  }
  const input = it.toolCall?.rawInput as { url?: unknown } | undefined;
  if (it.toolCall?.kind === "fetch" && typeof input?.url === "string") push(input.url);
  linksByItem.set(it, out);
  return out;
}

/** One link per host, from what the person wrote and what the agent fetched; first seen first. */
export function linkHosts(items: readonly TranscriptItem[]): LinkHost[] {
  const seen = new Map<string, string>();
  for (const it of items) {
    for (const link of itemLinks(it)) if (!seen.has(link.host)) seen.set(link.host, link.url);
  }
  return [...seen].map(([host, url]) => ({ host, url }));
}
