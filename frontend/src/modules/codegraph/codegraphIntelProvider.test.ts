import { beforeEach, describe, expect, it, mock } from "bun:test";
import type { CodeGraphDefinition, CodeGraphReference } from "@/bridge/codegraph";

let response: CodeGraphDefinition[] = [];
let implementations: CodeGraphDefinition[] = [];
let references: CodeGraphReference[] = [];
const calls: Array<{ filePath: string; line: number; identifier: string }> = [];

// Spread the real module, override the three this file is about.
//
// bun's mock.module replaces a module for the WHOLE run, not just this file,
// and a replacement that lists only what it needs deletes every other export
// from everyone else's view. Left partial, this made codegraphActions — loaded
// later, by another test file — fail to import getCodeGraphNeighbors, which
// read as a syntax error in a file that had nothing wrong with it.
const bridge = await import("@/bridge/codegraph");

mock.module("@/bridge/codegraph", () => ({
  ...bridge,
  findCodeGraphDefinitions: async (filePath: string, line: number, identifier: string) => {
    calls.push({ filePath, line, identifier });
    return response;
  },
  findCodeGraphImplementations: async (filePath: string, line: number, identifier: string) => {
    calls.push({ filePath, line, identifier });
    return implementations;
  },
  findCodeGraphReferences: async (filePath: string, line: number, identifier: string) => {
    calls.push({ filePath, line, identifier });
    return references;
  },
}));

const { createCodeGraphIntelProvider } = await import("./codegraphIntelProvider");

beforeEach(() => {
  response = [];
  implementations = [];
  references = [];
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

  it("maps an implementation like a definition, from the baseline line", async () => {
    implementations = [
      { nodeId: "com.acme.OrderServiceImpl.create(Request)", label: "com.acme.OrderServiceImpl.create", kind: "method", filePath: "OrderServiceImpl.java", line: 31 },
    ];
    const provider = createCodeGraphIntelProvider();
    const locations = await provider.implementation!(
      { path: "OrderService.java", line: 12, displayLine: 15, identifier: "create" },
      neverAborted(),
    );
    expect(calls).toEqual([{ filePath: "OrderService.java", line: 12, identifier: "create" }]);
    expect(locations).toEqual([
      { path: "OrderServiceImpl.java", line: 31, label: "OrderServiceImpl.create", kind: "method" },
    ]);
  });

  it("maps a reference to the line of the use, the method it sits in, and its source line", async () => {
    references = [
      { nodeId: "com.acme.OrderController.post(Request)", label: "com.acme.OrderController.post", kind: "call", filePath: "OrderController.java", line: 44, code: "return service.create(request);" },
    ];
    const provider = createCodeGraphIntelProvider();
    const locations = await provider.references!(
      { path: "OrderService.java", line: 12, displayLine: 12, identifier: "create" },
      neverAborted(),
    );
    expect(locations).toEqual([
      { path: "OrderController.java", line: 44, label: "OrderController.post", kind: "call", preview: "return service.create(request);" },
    ]);
  });

  it("labels an injection by the class that declares it, and omits an empty preview", async () => {
    references = [
      { nodeId: "com.acme.OrderController", label: "com.acme.OrderController", kind: "inject", filePath: "OrderController.java", line: 18, code: "" },
    ];
    const provider = createCodeGraphIntelProvider();
    const locations = await provider.references!(
      { path: "OrderService.java", line: 9, displayLine: 9, identifier: "OrderService" },
      neverAborted(),
    );
    expect(locations).toEqual([{ path: "OrderController.java", line: 18, label: "OrderController", kind: "inject" }]);
  });
});
