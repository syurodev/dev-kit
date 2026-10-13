import type { ServiceIndexStatus } from "@/bridge/workspace";
import type { FolderEntryPoints, FolderState } from "@/bridge/workspaceCode";

/**
 * How well a folder can be read through Code Graph, as one word and a tone.
 *
 * It is a per-folder fact, combined from three sources that each know a part:
 * the machine (is the folder there?), the index file (what is known, and from
 * when) and the queue (what is happening to it). The workspace header adds the
 * folders up; it never presents one number as if every folder shared it.
 */
export type Health =
  | "ready"
  | "limited"
  | "stale"
  | "indexing"
  | "pending"
  | "failed"
  | "needs-analyzer"
  | "unsupported"
  | "not-indexed"
  | "unavailable";

export type Tone = "ok" | "busy" | "warn" | "failed" | "neutral";

export type FolderHealth = { health: Health; tone: Tone; label: string; detail: string };

export function folderHealth(
  fs: FolderState | undefined,
  entries: FolderEntryPoints | undefined,
  status: ServiceIndexStatus | undefined,
  now: number,
): FolderHealth {
  if (fs && !fs.accessible) {
    return { health: "unavailable", tone: "failed", label: "Unavailable", detail: fs.error || "The folder cannot be read." };
  }
  const state = status?.state ?? "";
  if (state === "indexing") {
    return { health: "indexing", tone: "busy", label: "Indexing", detail: entries?.indexedAt ? `Previous index ${ago(entries.indexedAt, now)} stays available.` : "First index of this folder." };
  }
  if (state === "pending") {
    return { health: "pending", tone: "busy", label: "Queued", detail: "Waiting for an analyzer slot." };
  }
  if (state === "failed" || entries?.state === "failed") {
    const missing = status?.missingToolchain || entries?.missingToolchain;
    const error = status?.error || entries?.error || "";
    if (missing) {
      return { health: "needs-analyzer", tone: "warn", label: "Needs analyzer", detail: error || "Build or install the analyzer for this folder's language." };
    }
    return { health: "failed", tone: "failed", label: "Failed", detail: error || "The last run failed." };
  }
  if (state === "unsupported" || entries?.state === "unsupported") {
    return { health: "unsupported", tone: "neutral", label: "Files only", detail: "No analyzer reads this folder's language. Files, search and git still work." };
  }
  if (entries?.state === "indexing") {
    return { health: "indexing", tone: "busy", label: "Indexing", detail: "Opening this folder's index." };
  }
  if (entries?.state === "indexed") {
    if (entries.stale) {
      return { health: "stale", tone: "warn", label: "Refreshing", detail: `Showing the index from ${ago(entries.indexedAt, now)} while it refreshes.` };
    }
    const support = entries.entryPointSupport || "Entry point support was not reported by this index; absence is not proof that this folder has no entry points.";
    const warnings = entries.entryPointWarnings ?? [];
    if (warnings.length || !entries.entries.length) {
      return { health: "limited", tone: "warn", label: "Indexed · entry limits", detail: [support, ...warnings].join(" ") };
    }
    return { health: "ready", tone: "ok", label: "Indexed", detail: `${entries.indexedAt ? `Indexed ${ago(entries.indexedAt, now)}.` : "Indexed."} ${support}` };
  }
  if (entries?.state === "not-indexed") {
    return { health: "not-indexed", tone: "neutral", label: "Not indexed", detail: "No index yet. Reindex to build one." };
  }
  // Nothing has answered yet: say nothing rather than guess.
  return { health: "pending", tone: "neutral", label: "Checking", detail: "" };
}

export function ago(at: number, now: number): string {
  if (!at) return "at an unknown time";
  const s = Math.max(0, Math.round((now - at) / 1000));
  if (s < 45) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.round(h / 24)}d ago`;
}

export type HealthSummary = {
  total: number;
  counts: Record<Health, number>;
  /** "3 ready · 1 indexing · 1 failed". */
  text: string;
  tone: Tone;
};

const ORDER: Array<[Health, string]> = [
  ["ready", "ready"],
  ["limited", "entry limits"],
  ["indexing", "indexing"],
  ["pending", "queued"],
  ["stale", "refreshing"],
  ["failed", "failed"],
  ["needs-analyzer", "need an analyzer"],
  ["unavailable", "unavailable"],
  ["unsupported", "files only"],
  ["not-indexed", "not indexed"],
];

export function summarizeHealth(healths: readonly FolderHealth[]): HealthSummary {
  const counts = Object.fromEntries(ORDER.map(([h]) => [h, 0])) as Record<Health, number>;
  for (const h of healths) counts[h.health] += 1;
  const parts = ORDER.filter(([h]) => counts[h] > 0).map(([h, word]) => `${counts[h]} ${word}`);
  let tone: Tone = "ok";
  if (counts.failed || counts.unavailable) tone = "failed";
  else if (counts["needs-analyzer"] || counts.stale || counts.limited) tone = "warn";
  else if (counts.indexing || counts.pending) tone = "busy";
  else if (counts.ready === 0) tone = "neutral";
  return { total: healths.length, counts, text: parts.join(" · ") || "No folders", tone };
}

/** Progress of a reindex batch over the folders it covers: done / total, only while it runs. */
export function batchProgress(statuses: readonly (ServiceIndexStatus | undefined)[]): { done: number; total: number } | null {
  const running = statuses.filter((s) => s && (s.state === "pending" || s.state === "indexing")).length;
  if (running === 0) return null;
  return { done: statuses.length - running, total: statuses.length };
}
