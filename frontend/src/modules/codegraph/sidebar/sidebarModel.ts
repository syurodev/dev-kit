import type { CodeGraphEndpoint, CodeGraphIndexInfo, CodeGraphSymbolMatch } from "@/bridge/codegraph";
import type { CodeGraphPlanNode, CodeGraphPlanProgress } from "@/bridge/codegraphPlan";

/**
 * The Code Graph sidebar's shaping, pure (Figma `CodeGraph / Sidebar`,
 * 1192:69479). The backend hands over flat lists; everything the design groups,
 * counts or phrases is derived here so it can be tested without a bridge.
 */

// ── Endpoints ───────────────────────────────────────────────────────────────

export type ControllerRef = { id: string; fqn: string; name: string };

/**
 * The controller class an endpoint's handler is declared in, from its entry
 * node id — `com.acme.web.UsersController.getUser(java.lang.String)`. Cut at
 * the parameter list first: parameter types have dots of their own. A class
 * that shares its FQN with one in another module carries ` [module]`, which is
 * kept in the id and dropped from what is shown.
 *
 * An inherited handler lands on the base class that declares it — that is
 * where the code is.
 */
export function controllerOf(entryNode: string): ControllerRef {
  const head = entryNode.includes("(") ? entryNode.slice(0, entryNode.indexOf("(")) : entryNode;
  const id = head.includes(".") ? head.slice(0, head.lastIndexOf(".")) : head;
  const fqn = id.replace(/\s*\[[^\]]*\]$/, "");
  return { id, fqn, name: fqn.slice(fqn.lastIndexOf(".") + 1) };
}

const packageOf = (fqn: string) => (fqn.includes(".") ? fqn.slice(0, fqn.lastIndexOf(".")).split(".") : []);

export type ControllerGroup = { id: string; name: string; fqn: string; endpoints: CodeGraphEndpoint[] };
export type ServiceGroup = { key: string; label: string; controllers: ControllerGroup[]; count: number };

const METHOD_ORDER = ["GET", "POST", "PUT", "PATCH", "DELETE"];
const methodRank = (m: string) => {
  const i = METHOD_ORDER.indexOf(m.toUpperCase());
  return i === -1 ? METHOD_ORDER.length : i;
};

/**
 * Endpoints grouped by service, then controller.
 *
 * "Service" is the package segment right after the prefix every controller
 * shares — `com.acme.refund.web` and `com.acme.order.web` are the `refund` and
 * `order` services. The analyzer does not attach a build module to an
 * endpoint, and a package is what a reader recognises anyway. One service
 * comes back as one group: the caller skips the level rather than show a
 * single heading over everything.
 */
export function groupEndpoints(endpoints: CodeGraphEndpoint[]): ServiceGroup[] {
  const byController = new Map<string, ControllerGroup>();
  for (const ep of endpoints) {
    const c = controllerOf(ep.entryNode);
    let g = byController.get(c.id);
    if (!g) {
      g = { id: c.id, name: c.name, fqn: c.fqn, endpoints: [] };
      byController.set(c.id, g);
    }
    g.endpoints.push(ep);
  }
  const controllers = [...byController.values()];
  for (const c of controllers) {
    c.endpoints.sort((a, b) => a.path.localeCompare(b.path) || methodRank(a.httpMethod) - methodRank(b.httpMethod));
  }

  const packages = controllers.map((c) => packageOf(c.fqn));
  let shared = 0;
  if (packages.length > 1) {
    const shortest = Math.min(...packages.map((p) => p.length));
    while (shared < shortest && packages.every((p) => p[shared] === packages[0][shared])) shared++;
  }

  const services = new Map<string, ServiceGroup>();
  controllers.forEach((c, i) => {
    const pkg = packages[i];
    const key = pkg[shared] ?? pkg[pkg.length - 1] ?? "";
    let s = services.get(key);
    if (!s) {
      s = { key, label: key || "default", controllers: [], count: 0 };
      services.set(key, s);
    }
    s.controllers.push(c);
    s.count += c.endpoints.length;
  });
  const out = [...services.values()].sort((a, b) => a.label.localeCompare(b.label));
  for (const s of out) s.controllers.sort((a, b) => a.name.localeCompare(b.name));
  return out;
}

/** The methods present, in the order the filter chips show them. */
export function methodsIn(endpoints: CodeGraphEndpoint[]): string[] {
  return [...new Set(endpoints.map((e) => e.httpMethod.toUpperCase()))].sort((a, b) => methodRank(a) - methodRank(b));
}

/** The endpoint search: path, method or controller name. */
export function filterEndpoints(endpoints: CodeGraphEndpoint[], query: string, methods: ReadonlySet<string>): CodeGraphEndpoint[] {
  const needle = query.trim().toLowerCase();
  return endpoints.filter((ep) => {
    if (methods.size > 0 && !methods.has(ep.httpMethod.toUpperCase())) return false;
    if (!needle) return true;
    return (
      ep.path.toLowerCase().includes(needle) ||
      ep.httpMethod.toLowerCase() === needle ||
      controllerOf(ep.entryNode).name.toLowerCase().includes(needle)
    );
  });
}

// ── Index status ────────────────────────────────────────────────────────────

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

export type IndexTone = "ok" | "busy" | "failed";

/** The line under the project name, and the colour of its dot. */
export function indexStatusLine(
  info: Pick<CodeGraphIndexInfo, "indexedAt" | "indexing" | "error" | "stale"> | null,
  counts: { endpoints: number; services: number },
  now: number,
): { tone: IndexTone; text: string } {
  if (!info) return { tone: "busy", text: "Reading the index…" };
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
    `${counts.endpoints} endpoint${counts.endpoints === 1 ? "" : "s"}`,
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
