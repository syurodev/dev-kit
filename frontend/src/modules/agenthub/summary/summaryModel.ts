import type { CheckpointIndex } from "../checkpoints";
import type { PlanEntry, TranscriptItem } from "../types";

/** The agent's most recent plan in the session, or none. */
export function latestPlan(items: readonly TranscriptItem[]): PlanEntry[] {
  for (let i = items.length - 1; i >= 0; i -= 1) {
    const it = items[i];
    if (it.kind === "plan" && it.plan) return it.plan;
  }
  return [];
}

export type SessionChange = { path: string; added: number; removed: number; binary: boolean; checkpointId: string };

/** Every file the session touched, its lines summed over turns; the diff opens from the newest checkpoint that touched it. */
export function sessionChanges(index: CheckpointIndex): SessionChange[] {
  const records = [...index.values()]
    .filter((r) => r.changed?.length)
    .sort((a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt));
  const byPath = new Map<string, SessionChange>();
  for (const record of records) {
    for (const file of record.changed ?? []) {
      const prev = byPath.get(file.path);
      byPath.set(file.path, {
        path: file.path,
        added: (prev?.added ?? 0) + file.added,
        removed: (prev?.removed ?? 0) + file.removed,
        binary: Boolean(prev?.binary || file.binary),
        checkpointId: record.id,
      });
    }
  }
  return [...byPath.values()];
}

const DATA_MODULES: Record<string, string> = { db: "Database", kafka: "Kafka", ssh: "SSH" };
const WIRE = "mcp__devkit__";

function devkitPrefix(name: string): string | null {
  const plain = name.startsWith(WIRE) ? name.slice(WIRE.length).split("_")[0] : name.split(".")[0];
  return plain in DATA_MODULES ? plain : null;
}

export type ToolSource = { module: string; label: string; count: number };

/** DevKit data tools the agent called, counted per module. `module` is the module manifest id. */
export function toolSources(items: readonly TranscriptItem[]): ToolSource[] {
  const counts = new Map<string, number>();
  for (const it of items) {
    const name = it.toolCall?.name;
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

export type LinkHost = { host: string; url: string };

/** One link per host, from what the person wrote and what the agent fetched; first seen first. */
export function linkHosts(items: readonly TranscriptItem[]): LinkHost[] {
  const seen = new Map<string, string>();
  const add = (raw: string) => {
    try {
      const u = new URL(raw);
      if (!seen.has(u.host)) seen.set(u.host, raw);
    } catch {
      /* not a URL */
    }
  };
  for (const it of items) {
    if (it.kind === "user_message_chunk" && typeof it.text === "string") {
      for (const m of it.text.matchAll(URL_RE)) add(m[0]);
    }
    const input = it.toolCall?.rawInput as { url?: unknown } | undefined;
    if (it.toolCall?.kind === "fetch" && typeof input?.url === "string") add(input.url);
  }
  return [...seen].map(([host, url]) => ({ host, url }));
}
