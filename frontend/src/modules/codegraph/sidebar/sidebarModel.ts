import type { CodeGraphEntryKind, CodeGraphEntryPoint, CodeGraphIndexInfo, CodeGraphSymbolMatch } from "@/bridge/codegraph";
import type { CodeGraphPlanNode, CodeGraphPlanProgress } from "@/bridge/codegraphPlan";
import type { ServiceIndexStatus } from "@/bridge/workspace";

/**
 * The Code Graph sidebar's shaping, pure (Figma `CodeGraph / Sidebar`,
 * 1192:69479). The backend hands over flat lists; everything the design groups,
 * counts or phrases is derived here so it can be tested without a bridge.
 */

// ── Entry points ────────────────────────────────────────────────────────────

/**
 * Each kind of entry point, in the order the list shows them, and the heading
 * over it. What calls in decides the kind — a client over HTTP, a broker,
 * Kong's request cycle, a scheduler — and Go says which (entrypoints.go). A
 * kind this build does not know still lists, last, under its own name.
 */
const KINDS: { kind: CodeGraphEntryKind; label: string }[] = [
  { kind: "http", label: "HTTP" },
  { kind: "graphql", label: "GraphQL" },
  { kind: "rpc", label: "gRPC" },
  { kind: "message", label: "Messages" },
  { kind: "socket", label: "WebSocket" },
  { kind: "schedule", label: "Scheduled" },
  { kind: "plugin", label: "Kong plugins" },
];
const kindRank = (kind: string) => {
  const i = KINDS.findIndex((k) => k.kind === kind);
  return i === -1 ? KINDS.length : i;
};
export const kindLabel = (kind: string) => KINDS.find((k) => k.kind === kind)?.label ?? kind;

const METHOD_ORDER = ["GET", "POST", "PUT", "PATCH", "DELETE"];
const methodRank = (m: string) => {
  const i = METHOD_ORDER.indexOf(m.toUpperCase());
  return i === -1 ? METHOD_ORDER.length : i;
};

/** Within a group: the order Go gave (Kong's phases), then target, then method. */
function compareEntries(a: CodeGraphEntryPoint, b: CodeGraphEntryPoint): number {
  return (
    (a.order || Number.MAX_SAFE_INTEGER) - (b.order || Number.MAX_SAFE_INTEGER) ||
    a.target.localeCompare(b.target) ||
    methodRank(a.verb) - methodRank(b.verb) ||
    a.verb.localeCompare(b.verb)
  );
}

export type OwnerGroup = { key: string; label: string; owner: string; entries: CodeGraphEntryPoint[] };
export type ServiceGroup = { key: string; label: string; owners: OwnerGroup[]; count: number };
export type KindGroup = { key: string; kind: string; label: string; services: ServiceGroup[]; count: number };

/**
 * Entry points grouped by kind, then service, then what declares them — the
 * class, the Kong plugin, the route file. Go decides the service and the
 * owner; this only orders. A level with a single group is still returned as
 * one: the caller skips it rather than show one heading over everything.
 */
export function groupEntryPoints(entries: CodeGraphEntryPoint[]): KindGroup[] {
  const kinds = new Map<string, KindGroup>();
  for (const ep of entries) {
    let k = kinds.get(ep.kind);
    if (!k) {
      k = { key: `kind:${ep.kind}`, kind: ep.kind, label: kindLabel(ep.kind), services: [], count: 0 };
      kinds.set(ep.kind, k);
    }
    let s = k.services.find((x) => x.label === ep.service);
    if (!s) {
      s = { key: `svc:${ep.kind}:${ep.service}`, label: ep.service, owners: [], count: 0 };
      k.services.push(s);
    }
    let o = s.owners.find((x) => x.owner === ep.owner);
    if (!o) {
      o = { key: `own:${ep.kind}:${ep.owner}`, label: ep.group || ep.owner, owner: ep.owner, entries: [] };
      s.owners.push(o);
    }
    o.entries.push(ep);
    s.count++;
    k.count++;
  }
  const out = [...kinds.values()].sort((a, b) => kindRank(a.kind) - kindRank(b.kind) || a.kind.localeCompare(b.kind));
  for (const k of out) {
    k.services.sort((a, b) => a.label.localeCompare(b.label));
    for (const s of k.services) {
      s.owners.sort((a, b) => a.label.localeCompare(b.label) || a.owner.localeCompare(b.owner));
      for (const o of s.owners) o.entries.sort(compareEntries);
    }
  }
  return out;
}

/** How many services the list spans — 0 or 1 is one project, and says nothing. */
export function servicesIn(entries: CodeGraphEntryPoint[]): number {
  return new Set(entries.map((e) => e.service).filter(Boolean)).size;
}

/**
 * The verbs present, in the order the filter chips show them: by kind, then
 * the order a kind itself has — HTTP methods as REST reads them, Kong's phases
 * as Kong runs them.
 */
export function verbsIn(entries: CodeGraphEntryPoint[]): string[] {
  const rank = new Map<string, [number, number, number]>();
  for (const e of entries) {
    const verb = e.verb.toUpperCase();
    const r: [number, number, number] = [kindRank(e.kind), e.order || Number.MAX_SAFE_INTEGER, methodRank(verb)];
    const prev = rank.get(verb);
    if (!prev || r[0] < prev[0] || (r[0] === prev[0] && r[1] < prev[1])) rank.set(verb, r);
  }
  return [...rank.entries()]
    .sort(([va, a], [vb, b]) => a[0] - b[0] || a[1] - b[1] || a[2] - b[2] || va.localeCompare(vb))
    .map(([verb]) => verb);
}

/** The list's search: target, verb, or what declares it. */
export function filterEntryPoints(
  entries: CodeGraphEntryPoint[],
  query: string,
  verbs: ReadonlySet<string>,
): CodeGraphEntryPoint[] {
  const needle = query.trim().toLowerCase();
  return entries.filter((ep) => {
    if (verbs.size > 0 && !verbs.has(ep.verb.toUpperCase())) return false;
    if (!needle) return true;
    return (
      ep.target.toLowerCase().includes(needle) ||
      ep.verb.toLowerCase() === needle ||
      ep.group.toLowerCase().includes(needle)
    );
  });
}

// ── Index status ────────────────────────────────────────────────────────────

/**
 * The line a workspace service shows under its name in the project switcher:
 * where its index stands, the tone of the dot beside it. Nothing to say
 * (no status yet, or nothing on disk and nothing queued) is null.
 */
export function serviceIndexLine(
  status: ServiceIndexStatus | undefined,
  now: number,
): { text: string; tone: IndexTone } | null {
  switch (status?.state) {
    case "pending":
      return { text: "Queued to index", tone: "busy" };
    case "indexing":
      return { text: "Indexing…", tone: "busy" };
    case "indexed":
      return {
        text: status.indexedAt ? `Indexed ${relativeTime(status.indexedAt, now)}` : "Indexed",
        tone: "ok",
      };
    case "failed":
      return {
        text: status.missingToolchain ? "Not indexed — analyzer missing" : "Index failed",
        tone: "failed",
      };
    case "unsupported":
      return { text: `${NOT_SUPPORTED} · files only`, tone: "neutral" };
    default:
      return null;
  }
}

/** "just now", "2m ago", "3h ago", "yesterday", "4d ago". */
export function relativeTime(at: number, now: number): string {
  const s = Math.max(0, Math.round((now - at) / 1000));
  if (s < 45) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.round(h / 24);
  return d === 1 ? "yesterday" : `${d}d ago`;
}

/** `neutral` is a state, not an outcome: no analyzer reads the project, which is nobody's failure. */
export type IndexTone = "ok" | "busy" | "failed" | "neutral";

/** What a project no analyzer reads is labelled — a fact about it, not an error. */
export const NOT_SUPPORTED = "Not supported";

/** The line under the project name, and the colour of its dot. */
export function indexStatusLine(
  info: Pick<CodeGraphIndexInfo, "indexedAt" | "indexing" | "error" | "stale" | "unsupported"> | null,
  counts: { entryPoints: number; services: number },
  now: number,
): { tone: IndexTone; text: string } {
  if (!info) return { tone: "busy", text: "Reading the index…" };
  // Before indexing: nothing runs for such a project, and an optimistic
  // "indexing" from the open that found out must not outlive the answer.
  if (info.unsupported) return { tone: "neutral", text: `${NOT_SUPPORTED} · files only` };
  const when = info.indexedAt ? relativeTime(info.indexedAt, now) : "";
  // A reopen answering from the stored index while it refreshes: say which
  // index is on screen, the way a failed run does, rather than "Indexing…"
  // over a graph that already answers.
  if (info.indexing && info.stale) {
    return { tone: "busy", text: when ? `Refreshing · showing the index from ${when}` : "Refreshing…" };
  }
  if (info.indexing) return { tone: "busy", text: "Indexing…" };
  if (info.error) {
    return { tone: "failed", text: when ? `Index failed · showing the index from ${when}` : "Index failed" };
  }
  const parts = [
    when ? `Indexed ${when}` : "Indexed",
    `${counts.entryPoints} entry point${counts.entryPoints === 1 ? "" : "s"}`,
  ];
  if (counts.services > 1) parts.push(`${counts.services} services`);
  return { tone: "ok", text: parts.join(" · ") };
}

export type IndexFailure = { title: string; body: string; code: string };

/**
 * What the analyzer's failure means, from the error Go returns: the
 * subprocess's exit status and the tail of its output. The analyzer exits 3
 * out of memory, 4 on a stack overflow (analyzer Main.java); Go gives up after
 * ten minutes.
 */
export function classifyIndexError(error: string): IndexFailure {
  const exit = /exit status (\d+)/.exec(error)?.[1];
  const code = `codegraph.analyzerFailed${exit ? ` · exit ${exit}` : ""}`;
  const heap = /max heap (\d+)\s*MB/i.exec(error)?.[1];
  const files = /after ([\d,]+) files/i.exec(error)?.[1];
  if (exit === "3" || /out of memory|OutOfMemoryError/i.test(error)) {
    const mb = Number(heap);
    const limit = heap ? (mb >= 1024 ? `its ${Math.round(mb / 1024)} GB heap` : `its ${mb} MB heap`) : "its heap limit";
    return {
      title: "Analyzer ran out of memory",
      body: `The JVM hit ${limit}${files ? ` after ${files} files` : ""}. Excluding generated sources from the project usually brings it back under.`,
      code,
    };
  }
  if (exit === "4" || /StackOverflowError/i.test(error)) {
    return { title: "Analyzer hit a stack overflow", body: "A very deep expression or call chain stopped the analysis.", code };
  }
  if (/deadline exceeded|timed out|timeout/i.test(error)) {
    return { title: "Analysis timed out", body: "The analyzer ran for ten minutes without finishing.", code };
  }
  const first = error.split("\n")[0].replace(/\s*\(output:.*$/s, "");
  return { title: "Indexing failed", body: first || "The analyzer stopped with an error.", code };
}

// ── Symbols ─────────────────────────────────────────────────────────────────

const simpleType = (t: string) => t.trim().replace(/<.*>/, "").split(".").pop() ?? t;

/**
 * How a symbol reads in the results: a class by its simple name over its FQN;
 * a method as `Class.method` over its parameter types, taken from the node id.
 * The index has no return types, so none is shown.
 */
export function symbolDisplay(m: Pick<CodeGraphSymbolMatch, "nodeId" | "label" | "kind">): { name: string; detail: string } {
  if (m.kind === "class") {
    const fqn = m.label.replace(/\s*\[[^\]]*\]$/, "");
    return { name: fqn.slice(fqn.lastIndexOf(".") + 1), detail: fqn };
  }
  const parts = m.label.split(".");
  const name = parts.slice(-2).join(".");
  const params = /\(([^)]*)\)/.exec(m.nodeId)?.[1] ?? "";
  const detail = `(${params ? params.split(",").map(simpleType).join(", ") : ""})`;
  return { name, detail };
}

// ── Plan ────────────────────────────────────────────────────────────────────

export type PlanSummary = {
  title: string;
  total: number;
  done: number;
  needsChange: number;
  diverged: number;
  planned: number;
  sections: { key: "attention" | "planned" | "done"; label: string; nodes: CodeGraphPlanNode[] }[];
};

/**
 * The plan as a worklist: what needs attention (diverged, or waiting on a
 * change), what is still planned, what is done — in that order, empty
 * sections left out. The plan's title is its description's first line.
 */
export function planSummary(plan: CodeGraphPlanProgress, needle = ""): PlanSummary {
  const q = needle.trim().toLowerCase();
  const nodes = plan.nodes.filter((n) => !q || n.label.toLowerCase().includes(q) || n.fqn.toLowerCase().includes(q));
  const of = (...status: string[]) => nodes.filter((n) => status.includes(n.status));
  const all = (s: string) => plan.nodes.filter((n) => n.status === s).length;
  const sections: PlanSummary["sections"] = [
    { key: "attention" as const, label: "Needs attention", nodes: of("implemented_diverged", "pending_enhancement") },
    { key: "planned" as const, label: "Planned", nodes: of("not_implemented") },
    { key: "done" as const, label: "Done", nodes: of("implemented_matching") },
  ].filter((s) => s.nodes.length > 0);
  return {
    title: plan.description.split("\n")[0].trim() || "Plan",
    total: plan.nodes.length,
    done: all("implemented_matching"),
    needsChange: all("pending_enhancement"),
    diverged: all("implemented_diverged"),
    planned: all("not_implemented"),
    sections,
  };
}

// ── Projects ────────────────────────────────────────────────────────────────

/** `/Users/me/dev/acme-refunds` → name `acme-refunds`, where `~/dev`. */
export function projectLabel(path: string): { name: string; where: string } {
  const clean = path.replace(/[\\/]+$/, "");
  const cut = Math.max(clean.lastIndexOf("/"), clean.lastIndexOf("\\"));
  const name = cut === -1 ? clean : clean.slice(cut + 1);
  const parent = cut === -1 ? "" : clean.slice(0, cut);
  const where = parent.replace(/^\/(Users|home)\/[^/]+/, "~").replace(/^[A-Za-z]:\\Users\\[^\\]+/, "~");
  return { name, where: where || "/" };
}
