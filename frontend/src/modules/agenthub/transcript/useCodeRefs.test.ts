import { describe, expect, test } from "bun:test";
import type { FolderSymbols } from "@/bridge/workspaceCode";
import { createLimiter, lookupWorkspaceSymbol, symbolStaleTime, type SymbolLookupDeps } from "./useCodeRefs";

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((r) => (resolve = r));
  return { promise, resolve };
}

describe("createLimiter", () => {
  test("at most three at a time; the rest wait their turn", async () => {
    const limit = createLimiter(3);
    let inFlight = 0;
    let most = 0;
    const gates = Array.from({ length: 7 }, deferred);
    const runs = gates.map((gate) =>
      limit(async () => {
        inFlight++;
        most = Math.max(most, inFlight);
        await gate.promise;
        inFlight--;
      }),
    );
    await tick();
    expect(inFlight).toBe(3);
    for (const gate of gates) {
      gate.resolve();
      await tick();
    }
    await Promise.all(runs);
    expect(most).toBe(3);
    expect(inFlight).toBe(0);
  });

  test("work whose signal aborted while queued never runs", async () => {
    const limit = createLimiter(1);
    const first = deferred();
    const ran: string[] = [];
    const a = limit(async () => {
      ran.push("a");
      await first.promise;
    });
    const controller = new AbortController();
    const b = limit(async () => {
      ran.push("b");
    }, controller.signal);
    const c = limit(async () => {
      ran.push("c");
    });
    controller.abort();
    await expect(b).rejects.toBeDefined();
    first.resolve();
    await Promise.all([a, c]);
    expect(ran).toEqual(["a", "c"]);
  });

  test("an already aborted signal never runs", async () => {
    const limit = createLimiter(3);
    let ran = false;
    await expect(
      limit(async () => {
        ran = true;
      }, AbortSignal.abort()),
    ).rejects.toBeDefined();
    expect(ran).toBe(false);
  });
});

const folder = (folderId: string, state: FolderSymbols["state"], labels: string[]): FolderSymbols => ({
  folderId,
  state,
  matches: labels.map((label, i) => ({ nodeId: `${folderId}:${i}`, label, kind: "method", layer: "", filePath: `${folderId}/f${i}`, line: i + 1 })),
});

const ALL_LANGUAGES = [
  folder("java", "indexed", ["com.acme.OrderService.create"]),
  folder("ts", "indexed", ["src/users/users.controller.UsersController.create"]),
  folder("lua", "indexed", ["kong.plugins.x.handler.access"]),
];

function deps(results: FolderSymbols[], over: Partial<SymbolLookupDeps> = {}): SymbolLookupDeps {
  return {
    search: async () => results,
    folders: async () => [
      { id: "java", name: "orders" },
      { id: "ts", name: "users" },
      { id: "lua", name: "gateway" },
    ],
    ...over,
  };
}

describe("lookupWorkspaceSymbol", () => {
  test("Java, TypeScript and Lua labels resolve by their qualified names", async () => {
    const d = deps(ALL_LANGUAGES);
    const java = await lookupWorkspaceSymbol("ws", "OrderService.create", undefined, d);
    expect(java).toEqual({
      pending: false,
      hit: { workspaceId: "ws", folderId: "java", folderName: "orders", filePath: "java/f0", line: 1, kind: "method", label: "com.acme.OrderService.create" },
    });
    expect((await lookupWorkspaceSymbol("ws", "UsersController.create()", undefined, d)).hit?.folderName).toBe("users");
    expect((await lookupWorkspaceSymbol("ws", "handler.access", undefined, d)).hit?.folderName).toBe("gateway");
  });

  test("a bare name two folders define is no hit, and final", async () => {
    expect(await lookupWorkspaceSymbol("ws", "create", undefined, deps(ALL_LANGUAGES))).toEqual({ hit: null, pending: false });
  });

  test("searches by the last segment", async () => {
    const queries: string[] = [];
    await lookupWorkspaceSymbol("ws", "OrderService.create()", undefined, deps(ALL_LANGUAGES, {
      search: async (_ws, _folders, query) => {
        queries.push(query);
        return ALL_LANGUAGES;
      },
    }));
    expect(queries).toEqual(["create"]);
  });

  test("no hit while a folder is still indexing, not indexed or failed is pending", async () => {
    for (const state of ["indexing", "not-indexed", "failed"] as const) {
      const results = [folder("java", "indexed", []), folder("ts", state, [])];
      expect(await lookupWorkspaceSymbol("ws", "OrderService.create", undefined, deps(results))).toEqual({ hit: null, pending: true });
    }
    // A language nothing can index is an answer, not a wait.
    const unsupported = [folder("java", "indexed", []), folder("tf", "unsupported", [])];
    expect((await lookupWorkspaceSymbol("ws", "OrderService.create", undefined, deps(unsupported))).pending).toBe(false);
  });

  test("a burst runs through the bridge three at a time", async () => {
    let inFlight = 0;
    let most = 0;
    const gate = deferred();
    const search = async () => {
      inFlight++;
      most = Math.max(most, inFlight);
      await gate.promise;
      inFlight--;
      return ALL_LANGUAGES;
    };
    const runs = Array.from({ length: 8 }, (_, i) => lookupWorkspaceSymbol("ws", `Name${i}.create`, undefined, deps([], { search })));
    await tick();
    expect(inFlight).toBe(3);
    gate.resolve();
    await Promise.all(runs);
    expect(most).toBe(3);
  });
});

describe("symbolStaleTime", () => {
  test("five minutes for an answer, fifteen seconds while the index is not ready", () => {
    expect(symbolStaleTime({ hit: null, pending: false })).toBe(300_000);
    expect(symbolStaleTime({ hit: null, pending: true })).toBe(15_000);
    expect(symbolStaleTime(undefined)).toBe(300_000);
  });
});
