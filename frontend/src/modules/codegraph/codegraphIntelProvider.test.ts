import { beforeEach, describe, expect, it, mock } from "bun:test";
import type { CodeGraphDefinition } from "@/bridge/codegraph";

let response: CodeGraphDefinition[] = [];
const calls: Array<{ filePath: string; line: number; identifier: string }> = [];

mock.module("@/bridge/codegraph", () => ({
  findCodeGraphDefinitions: async (filePath: string, line: number, identifier: string) => {
    calls.push({ filePath, line, identifier });
    return response;
  },
}));

const { createCodeGraphIntelProvider } = await import("./codegraphIntelProvider");

beforeEach(() => {
  response = [];
  calls.length = 0;
});

function neverAborted(): AbortSignal {
  return new AbortController().signal;
}

describe("createCodeGraphIntelProvider", () => {
  it("sends the request's baseline line and identifier straight to the bridge", async () => {
    const provider = createCodeGraphIntelProvider();
    await provider.definition!(
      { path: "OrderService.java", line: 10, displayLine: 14, identifier: "validate" },
      neverAborted(),
    );
    expect(calls).toEqual([{ filePath: "OrderService.java", line: 10, identifier: "validate" }]);
  });

  it("maps a definition to a CodeLocation with a shortened label", async () => {
    response = [
      { nodeId: "com.acme.OrderService.validate", label: "com.acme.OrderService.validate", kind: "method", filePath: "OrderService.java", line: 40 },
    ];
    const provider = createCodeGraphIntelProvider();
    const locations = await provider.definition!(
      { path: "OrderService.java", line: 15, displayLine: 15, identifier: "validate" },
      neverAborted(),
    );
    expect(locations).toEqual([
      { path: "OrderService.java", line: 40, label: "OrderService.validate", kind: "method" },
    ]);
  });

  it("maps every definition when several come back (an ambiguous overload)", async () => {
    response = [
      { nodeId: "com.acme.OrderService.helper(int)", label: "com.acme.OrderService.helper", kind: "method", filePath: "OrderService.java", line: 25 },
      { nodeId: "com.acme.OrderService.helper(String)", label: "com.acme.OrderService.helper", kind: "method", filePath: "OrderService.java", line: 27 },
    ];
    const provider = createCodeGraphIntelProvider();
    const locations = await provider.definition!(
      { path: "OrderService.java", line: 15, displayLine: 15, identifier: "helper" },
      neverAborted(),
    );
    expect(locations).toHaveLength(2);
    expect(locations.map((l) => l.line)).toEqual([25, 27]);
    expect(locations.every((l) => l.label === "OrderService.helper")).toBe(true);
  });

  it("keeps a collision-qualified label intact rather than mangling it", async () => {
    response = [
      {
        nodeId: "com.acme.Application [services_billing]",
        label: "com.acme.Application [services_billing]",
        kind: "class",
        filePath: "billing/Application.java",
        line: 1,
      },
    ];
    const provider = createCodeGraphIntelProvider();
    const locations = await provider.definition!(
      { path: "Caller.java", line: 5, displayLine: 5, identifier: "Application" },
      neverAborted(),
    );
    expect(locations[0].label).toBe("Application [services_billing]");
  });

  it("returns an empty result rather than nothing found for an empty index answer", async () => {
    response = [];
    const provider = createCodeGraphIntelProvider();
    const locations = await provider.definition!(
      { path: "OrderService.java", line: 15, displayLine: 15, identifier: "NoSuchSymbol" },
      neverAborted(),
    );
    expect(locations).toEqual([]);
  });

  it("drops a result that arrives after the caller aborted", async () => {
    response = [
      { nodeId: "com.acme.OrderService.validate", label: "com.acme.OrderService.validate", kind: "method", filePath: "OrderService.java", line: 40 },
    ];
    const controller = new AbortController();
    controller.abort();
    const provider = createCodeGraphIntelProvider();
    const locations = await provider.definition!(
      { path: "OrderService.java", line: 15, displayLine: 15, identifier: "validate" },
      controller.signal,
    );
    // The bridge call itself already ran (it cannot be cancelled) — this
    // proves the provider still discards what it got back rather than
    // handing a stale answer to the caller.
    expect(calls).toHaveLength(1);
    expect(locations).toEqual([]);
  });
});
