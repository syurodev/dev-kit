import { describe, expect, it } from "bun:test";
import { buildGroups } from "./paletteSearch";
import type { CodeGraphEntryPoint, CodeGraphFileEntry, CodeGraphSymbolMatch } from "@/bridge/codegraph";

const ENDPOINTS: CodeGraphEntryPoint[] = [
  { id: "e1", verb: "GET", target: "/orders/{id}", entryNode: "com.acme.OrdersController.get", kind: "http", service: "", owner: "com.acme.OrdersController", group: "OrdersController", order: 0 },
  { id: "e2", verb: "POST", target: "/orders", entryNode: "com.acme.OrdersController.create", kind: "http", service: "", owner: "com.acme.OrdersController", group: "OrdersController", order: 0 },
];

const FILES: CodeGraphFileEntry[] = [
  { path: "src/main/java/com/acme/OrdersController.java", isDir: false },
  { path: "src/main/java/com/acme", isDir: true },
  { path: "src/main/java/com/acme/OrdersService.java", isDir: false },
];

const SYMBOLS: CodeGraphSymbolMatch[] = [
  {
    nodeId: "com.acme.OrdersService.create(Request)",
    label: "com.acme.OrdersService.create",
    kind: "method",
    layer: "service",
    filePath: "src/main/java/com/acme/OrdersService.java",
    line: 10,
  },
];

function actions() {
  const opened: string[] = [];
  return {
    opened,
    actions: {
      openEntryPoint: (ep: CodeGraphEntryPoint) => opened.push(`entry:${ep.id}`),
      openFile: (path: string) => opened.push(`file:${path}`),
      openSymbol: (match: CodeGraphSymbolMatch) => opened.push(`symbol:${match.nodeId}`),
    },
  };
}

describe("codegraph buildGroups", () => {
  it("returns nothing for an empty query, however much is loaded", () => {
    const { actions: acts } = actions();
    expect(buildGroups("", { entryPoints: ENDPOINTS, files: FILES, symbols: SYMBOLS }, acts)).toEqual([]);
  });

  it("matches an endpoint by its path", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("orders/{id}", { entryPoints: ENDPOINTS, files: [], symbols: [] }, acts);
    expect(groups.map((g) => g.heading)).toEqual(["ENTRY POINTS"]);
    expect(groups[0].items).toHaveLength(1);
    expect(groups[0].items[0].title).toBe("GET /orders/{id}");
  });

  it("lists a file by its name, with the directory as the subtitle, and skips directories", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("Controller", { entryPoints: [], files: FILES, symbols: [] }, acts);
    const files = groups.find((g) => g.heading === "FILES");
    expect(files?.items).toHaveLength(1);
    expect(files?.items[0].title).toBe("OrdersController.java");
    expect(files?.items[0].subtitle).toBe("src/main/java/com/acme");
  });

  it("shows a symbol by its short name, detailed by the caller-supplied match set", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("create", { entryPoints: [], files: [], symbols: SYMBOLS }, acts);
    const symbols = groups.find((g) => g.heading === "SYMBOLS");
    expect(symbols?.items).toHaveLength(1);
    expect(symbols?.items[0].title).toBe("OrdersService.create");
  });

  it("omits a group with nothing in it rather than an empty heading", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("zzz-nomatch", { entryPoints: ENDPOINTS, files: FILES, symbols: SYMBOLS }, acts);
    expect(groups).toEqual([]);
  });

  it("runs the caller's action for the picked row", () => {
    const { actions: acts, opened } = actions();
    const endpointGroups = buildGroups("GET /orders", { entryPoints: ENDPOINTS, files: [], symbols: [] }, acts);
    endpointGroups[0].items[0].run();
    const fileGroups = buildGroups("Controller", { entryPoints: [], files: FILES, symbols: [] }, acts);
    fileGroups[0].items[0].run();
    expect(opened).toEqual(["entry:e1", "file:src/main/java/com/acme/OrdersController.java"]);
  });
});
