import { describe, expect, it } from "bun:test";
import { buildGroups, type PaletteEntry, type PaletteFile, type PaletteSymbol } from "./paletteSearch";
import type { CodeGraphEntryPoint, CodeGraphSymbolMatch } from "@/bridge/codegraph";

const ep = (id: string, verb: string, target: string): CodeGraphEntryPoint => ({
  id,
  verb,
  target,
  entryNode: `com.acme.OrdersController.${id}`,
  kind: "http",
  service: "",
  owner: "com.acme.OrdersController",
  group: "OrdersController",
  order: 0,
});

const A = { folderId: "fa", folderLabel: "orders" };
const B = { folderId: "fb", folderLabel: "billing" };

const ENDPOINTS: PaletteEntry[] = [
  { ...A, ep: ep("e1", "GET", "/orders/{id}") },
  { ...A, ep: ep("e2", "POST", "/orders") },
];

const FILES: PaletteFile[] = [
  { ...A, path: "src/main/java/com/acme/OrdersController.java" },
  { ...A, path: "src/main/java/com/acme/OrdersService.java" },
];

const MATCH: CodeGraphSymbolMatch = {
  nodeId: "com.acme.OrdersService.create(Request)",
  label: "com.acme.OrdersService.create",
  kind: "method",
  layer: "service",
  filePath: "src/main/java/com/acme/OrdersService.java",
  line: 10,
};
const SYMBOLS: PaletteSymbol[] = [{ ...A, match: MATCH }];

function actions() {
  const opened: string[] = [];
  return {
    opened,
    actions: {
      openEntryPoint: (folderId: string, e: CodeGraphEntryPoint) => opened.push(`entry:${folderId}:${e.id}`),
      openFile: (folderId: string, path: string) => opened.push(`file:${folderId}:${path}`),
      openSymbol: (folderId: string, match: CodeGraphSymbolMatch) => opened.push(`symbol:${folderId}:${match.nodeId}`),
    },
  };
}

describe("codegraph buildGroups", () => {
  it("returns nothing for an empty query, however much is loaded", () => {
    const { actions: acts } = actions();
    expect(buildGroups("", { entryPoints: ENDPOINTS, files: FILES, symbols: SYMBOLS }, acts)).toEqual([]);
  });

  it("matches an entry point by its path", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("orders/{id}", { entryPoints: ENDPOINTS, files: [], symbols: [] }, acts);
    expect(groups.map((g) => g.heading)).toEqual(["ENTRY POINTS"]);
    expect(groups[0].items).toHaveLength(1);
    expect(groups[0].items[0].title).toBe("GET /orders/{id}");
    expect(groups[0].items[0].id).toBe("entry:fa:e1");
  });

  it("lists a file by its name, with the directory as the subtitle", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("Controller", { entryPoints: [], files: FILES, symbols: [] }, acts);
    const files = groups.find((g) => g.heading === "FILES");
    expect(files?.items).toHaveLength(1);
    expect(files?.items[0].title).toBe("OrdersController.java");
    expect(files?.items[0].subtitle).toBe("src/main/java/com/acme");
  });

  it("names the folder of each result only when several folders are searched", () => {
    const { actions: acts } = actions();
    const files: PaletteFile[] = [
      { ...A, path: "src/config.ts" },
      { ...B, path: "src/config.ts" },
    ];
    const single = buildGroups("config", { entryPoints: [], files: [files[0]], symbols: [] }, acts);
    expect(single[0].items[0].subtitle).toBe("src");

    const multi = buildGroups("config", { entryPoints: [], files, symbols: [], multi: true }, acts);
    const items = multi[0].items;
    expect(items).toHaveLength(2);
    expect(items.map((i) => i.subtitle).sort()).toEqual(["billing › src", "orders › src"]);
    // The same relative path in two folders is two results, with two ids.
    expect(new Set(items.map((i) => i.id)).size).toBe(2);
  });

  it("shows a symbol by its short name", () => {
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

  it("runs the caller's action with the folder of the picked row", () => {
    const { actions: acts, opened } = actions();
    buildGroups("GET /orders", { entryPoints: ENDPOINTS, files: [], symbols: [] }, acts)[0].items[0].run();
    buildGroups("Controller", { entryPoints: [], files: FILES, symbols: [] }, acts)[0].items[0].run();
    buildGroups("create", { entryPoints: [], files: [], symbols: SYMBOLS }, acts)[0].items[0].run();
    expect(opened).toEqual([
      "entry:fa:e1",
      "file:fa:src/main/java/com/acme/OrdersController.java",
      "symbol:fa:com.acme.OrdersService.create(Request)",
    ]);
  });
});
