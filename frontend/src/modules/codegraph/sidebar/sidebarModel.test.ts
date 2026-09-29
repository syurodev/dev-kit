import { describe, expect, it } from "bun:test";
import type { CodeGraphEntryPoint } from "@/bridge/codegraph";
import {
  classifyIndexError,
  filterEntryPoints,
  groupEntryPoints,
  indexStatusLine,
  planSummary,
  projectLabel,
  relativeTime,
  serviceIndexLine,
  servicesIn,
  symbolDisplay,
  verbsIn,
} from "./sidebarModel";

const ep = (verb: string, target: string, over: Partial<CodeGraphEntryPoint> = {}): CodeGraphEntryPoint => ({
  id: `${verb} ${target}`,
  verb,
  target,
  entryNode: "",
  kind: "http",
  service: "",
  owner: "",
  group: "",
  order: 0,
  ...over,
});
const refund = { service: "refund", owner: "com.acme.refund.web.RefundController", group: "RefundController" };
const refundAdmin = { service: "refund", owner: "com.acme.refund.web.RefundAdminController", group: "RefundAdminController" };
const order = { service: "order", owner: "com.acme.order.web.OrderController", group: "OrderController" };

const java = [
  ep("POST", "/refunds", refund),
  ep("GET", "/refunds/{id}", refund),
  ep("GET", "/admin/refunds", refundAdmin),
  ep("DELETE", "/orders/{id}", order),
  ep("GET", "/orders/{id}", order),
];

// A Kong repository: two plugins' phases, and one plugin's Admin API.
const plugin = (phase: string, name: string, order: number) =>
  ep(phase, name, { kind: "plugin", owner: `kong.plugins.${name}.handler`, group: name, order });
const kong = [
  plugin("LOG", "rate-limit", 15),
  plugin("ACCESS", "rate-limit", 6),
  plugin("HEADER_FILTER", "rate-limit", 11),
  plugin("ACCESS", "auth", 6),
  ep("GET", "/rate-limit/status", { owner: "kong.plugins.rate-limit.api", group: "rate-limit" }),
];

describe("entry points", () => {
  it("groups by service, then by what declares them, in REST order", () => {
    const [http] = groupEntryPoints(java);
    expect(http.services.map((s) => [s.label, s.count])).toEqual([["order", 2], ["refund", 3]]);
    expect(http.services[1].owners.map((o) => o.label)).toEqual(["RefundAdminController", "RefundController"]);
    expect(http.services[0].owners[0].entries.map((e) => e.verb)).toEqual(["GET", "DELETE"]);
    expect(servicesIn(java)).toBe(2);
  });

  it("puts a Kong plugin's phases under it in the order Kong runs them, apart from its Admin API", () => {
    const groups = groupEntryPoints(kong);
    expect(groups.map((g) => [g.label, g.count])).toEqual([["HTTP", 1], ["Kong plugins", 4]]);
    const plugins = groups[1].services[0].owners;
    expect(plugins.map((o) => o.label)).toEqual(["auth", "rate-limit"]);
    expect(plugins[1].entries.map((e) => e.verb)).toEqual(["ACCESS", "HEADER_FILTER", "LOG"]);
    expect(servicesIn(kong)).toBe(0);
  });

  it("offers a chip per verb present — HTTP first, phases in run order", () => {
    expect(verbsIn(java)).toEqual(["GET", "POST", "DELETE"]);
    expect(verbsIn(kong)).toEqual(["GET", "ACCESS", "HEADER_FILTER", "LOG"]);
  });

  it("filters by verb chips, and by target or what declares it", () => {
    expect(filterEntryPoints(java, "", new Set(["DELETE"]))).toHaveLength(1);
    expect(filterEntryPoints(java, "admin", new Set())).toHaveLength(1);
    expect(filterEntryPoints(java, "ordercontroller", new Set())).toHaveLength(2);
    expect(filterEntryPoints(kong, "rate", new Set(["ACCESS"]))).toHaveLength(1);
  });

  it("lists a kind it does not know under its own name, last", () => {
    const groups = groupEntryPoints([ep("PAGE", "/about", { kind: "page", owner: "app/about", group: "/about" }), ...java]);
    expect(groups.map((g) => g.label)).toEqual(["HTTP", "page"]);
  });
});

describe("index status", () => {
  const now = 10_000_000;
  it("says when, how many, and how many services", () => {
    expect(relativeTime(now - 120_000, now)).toBe("2m ago");
    expect(indexStatusLine({ indexedAt: now - 120_000, indexing: false, error: "" }, { entryPoints: 142, services: 3 }, now)).toEqual({
      tone: "ok",
      text: "Indexed 2m ago · 142 entry points · 3 services",
    });
  });

  it("says which index answers while a reopen refreshes it", () => {
    const refreshing = { indexedAt: now - 3_600_000, indexing: true, error: "", stale: true };
    expect(indexStatusLine(refreshing, { entryPoints: 5, services: 1 }, now)).toEqual({
      tone: "busy",
      text: "Refreshing · showing the index from 1h ago",
    });
    expect(indexStatusLine({ ...refreshing, stale: false }, { entryPoints: 5, services: 1 }, now).text).toBe("Indexing…");
  });

  it("labels a project no analyzer reads, even over an optimistic indexing flag", () => {
    // ProjectHeader forces indexing on while the open that finds this out runs.
    expect(indexStatusLine({ indexedAt: 0, indexing: true, error: "", unsupported: true }, { entryPoints: 0, services: 1 }, now)).toEqual({
      tone: "neutral",
      text: "Not supported · files only",
    });
  });

  it("keeps saying which index is on screen when a run failed", () => {
    expect(indexStatusLine({ indexedAt: now - 2 * 3_600_000, indexing: false, error: "x" }, { entryPoints: 1, services: 1 }, now).text).toBe(
      "Index failed · showing the index from 2h ago",
    );
  });

  it("reads an out-of-memory exit from the analyzer's output", () => {
    const f = classifyIndexError(
      "java analyzer subprocess failed: exit status 3 (output: ran out of memory (max heap 2048 MB) after 14,208 files)",
    );
    expect(f.title).toBe("Analyzer ran out of memory");
    expect(f.body).toContain("its 2 GB heap after 14,208 files");
    expect(f.code).toBe("codegraph.analyzerFailed · exit 3");
  });
});

describe("symbols", () => {
  it("shows classes by name over their FQN, methods over their parameter types", () => {
    expect(symbolDisplay({ nodeId: "com.acme.refund.RefundService", label: "com.acme.refund.RefundService", kind: "class" })).toEqual({
      name: "RefundService",
      detail: "com.acme.refund.RefundService",
    });
    expect(
      symbolDisplay({
        nodeId: "com.acme.refund.RefundService.process(com.acme.refund.RefundRequest, java.util.List<java.lang.String>)",
        label: "com.acme.refund.RefundService.process",
        kind: "method",
      }),
    ).toEqual({ name: "RefundService.process", detail: "(RefundRequest, List)" });
  });
});

describe("plan", () => {
  it("is a worklist: attention, planned, done", () => {
    const node = (label: string, status: string) => ({ planNodeId: label, label, description: "", fqn: label, kind: "method", status, realNodeId: "" });
    const s = planSummary({
      description: "Refund notifications\nmore",
      state: "draft",
      aggregate: "in_progress",
      nodes: [node("a", "implemented_matching"), node("b", "implemented_diverged"), node("c", "not_implemented"), node("d", "pending_enhancement")],
    } as never);
    expect(s.title).toBe("Refund notifications");
    expect([s.total, s.done, s.diverged, s.needsChange, s.planned]).toEqual([4, 1, 1, 1, 1]);
    expect(s.sections.map((x) => [x.key, x.nodes.map((n) => n.label)])).toEqual([
      ["attention", ["b", "d"]],
      ["planned", ["c"]],
      ["done", ["a"]],
    ]);
  });
});

describe("projects", () => {
  it("names a project by its folder, and says where it is from home", () => {
    expect(projectLabel("/Users/me/dev/acme-refunds-service")).toEqual({ name: "acme-refunds-service", where: "~/dev" });
  });
});

describe("serviceIndexLine", () => {
  const base = { path: "/ws/a", error: "", missingToolchain: false, indexedAt: 0 };
  const now = 1_000_000_000;

  it("says nothing without a status", () => {
    expect(serviceIndexLine(undefined, now)).toBeNull();
    expect(serviceIndexLine({ ...base, state: "" }, now)).toBeNull();
  });

  it("queued and running are busy", () => {
    expect(serviceIndexLine({ ...base, state: "pending" }, now)).toEqual({ text: "Queued to index", tone: "busy" });
    expect(serviceIndexLine({ ...base, state: "indexing" }, now)?.tone).toBe("busy");
  });

  it("indexed says when", () => {
    expect(serviceIndexLine({ ...base, state: "indexed", indexedAt: now - 120_000 }, now)?.text).toBe("Indexed 2m ago");
  });

  it("a missing toolchain is told apart from a failure", () => {
    expect(serviceIndexLine({ ...base, state: "failed", missingToolchain: true }, now)?.text).toBe(
      "Not indexed — analyzer missing",
    );
    expect(serviceIndexLine({ ...base, state: "failed" }, now)?.text).toBe("Index failed");
    // No analyzer for a folder is a label, not a failure: no red dot.
    expect(serviceIndexLine({ ...base, state: "unsupported" }, now)).toEqual({
      text: "Not supported · files only",
      tone: "neutral",
    });
  });
});
