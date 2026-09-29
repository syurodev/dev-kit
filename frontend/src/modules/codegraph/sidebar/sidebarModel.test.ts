import { describe, expect, it } from "bun:test";
import {
  classifyIndexError,
  controllerOf,
  filterEndpoints,
  groupEndpoints,
  indexStatusLine,
  methodsIn,
  planSummary,
  projectLabel,
  relativeTime,
  serviceIndexLine,
  symbolDisplay,
} from "./sidebarModel";

const ep = (httpMethod: string, path: string, entryNode: string) => ({ id: `${httpMethod} ${path}`, httpMethod, path, entryNode });

const endpoints = [
  ep("POST", "/refunds", "com.acme.refund.web.RefundController.create(com.acme.refund.RefundRequest)"),
  ep("GET", "/refunds/{id}", "com.acme.refund.web.RefundController.get(java.lang.String)"),
  ep("GET", "/admin/refunds", "com.acme.refund.web.RefundAdminController.list()"),
  ep("DELETE", "/orders/{id}", "com.acme.order.web.OrderController.delete(java.util.UUID)"),
  ep("GET", "/orders/{id}", "com.acme.order.web.OrderController.get(java.util.UUID)"),
];

describe("endpoints", () => {
  it("finds the controller class, parameters and module suffix aside", () => {
    expect(controllerOf("com.acme.web.UsersController.getUser(java.lang.String)")).toEqual({
      id: "com.acme.web.UsersController",
      fqn: "com.acme.web.UsersController",
      name: "UsersController",
    });
    expect(controllerOf("com.acme.Health [orders].check()").name).toBe("Health");
  });

  it("groups by the package segment after the shared prefix, then by controller", () => {
    const groups = groupEndpoints(endpoints);
    expect(groups.map((g) => [g.label, g.count])).toEqual([["order", 2], ["refund", 3]]);
    expect(groups[1].controllers.map((c) => c.name)).toEqual(["RefundAdminController", "RefundController"]);
    expect(groups[0].controllers[0].endpoints.map((e) => e.httpMethod)).toEqual(["GET", "DELETE"]);
  });

  it("puts one package's controllers in a single group", () => {
    expect(groupEndpoints(endpoints.slice(0, 3))).toHaveLength(1);
  });

  it("filters by method chips and by path or controller", () => {
    expect(methodsIn(endpoints)).toEqual(["GET", "POST", "DELETE"]);
    expect(filterEndpoints(endpoints, "", new Set(["DELETE"]))).toHaveLength(1);
    expect(filterEndpoints(endpoints, "admin", new Set())).toHaveLength(1);
    expect(filterEndpoints(endpoints, "ordercontroller", new Set())).toHaveLength(2);
  });
});

describe("index status", () => {
  const now = 10_000_000;
  it("says when, how many, and how many services", () => {
    expect(relativeTime(now - 120_000, now)).toBe("2m ago");
    expect(indexStatusLine({ indexedAt: now - 120_000, indexing: false, error: "" }, { endpoints: 142, services: 3 }, now)).toEqual({
      tone: "ok",
      text: "Indexed 2m ago · 142 endpoints · 3 services",
    });
  });

  it("says which index answers while a reopen refreshes it", () => {
    const refreshing = { indexedAt: now - 3_600_000, indexing: true, error: "", stale: true };
    expect(indexStatusLine(refreshing, { endpoints: 5, services: 1 }, now)).toEqual({
      tone: "busy",
      text: "Refreshing · showing the index from 1h ago",
    });
    expect(indexStatusLine({ ...refreshing, stale: false }, { endpoints: 5, services: 1 }, now).text).toBe("Indexing…");
  });

  it("labels a project no analyzer reads, even over an optimistic indexing flag", () => {
    // ProjectHeader forces indexing on while the open that finds this out runs.
    expect(indexStatusLine({ indexedAt: 0, indexing: true, error: "", unsupported: true }, { endpoints: 0, services: 1 }, now)).toEqual({
      tone: "neutral",
      text: "Not supported · files only",
    });
  });

  it("keeps saying which index is on screen when a run failed", () => {
    expect(indexStatusLine({ indexedAt: now - 2 * 3_600_000, indexing: false, error: "x" }, { endpoints: 1, services: 1 }, now).text).toBe(
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
