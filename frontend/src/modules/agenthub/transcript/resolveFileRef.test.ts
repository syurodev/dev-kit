import { describe, expect, test } from "bun:test";
import { fileRefQuery, joinPath, normalizePath, resolveFileRef, withLine, type ResolverDeps } from "./resolveFileRef";

const ctx = { sessionId: "s1", workspaceId: "ws", projectPath: "/w/api" };
const FOLDERS = [
  { id: "api", name: "api", path: "/w/api" },
  { id: "web", name: "web", path: "/w/web" },
];

function fakeDeps(opts: { files?: string[]; byName?: string[]; unreadable?: Record<string, string> } = {}) {
  const calls = { folders: 0, stat: 0, byName: 0 };
  const files = new Set(opts.files ?? []);
  const deps: ResolverDeps = {
    folders: async () => {
      calls.folders++;
      return FOLDERS;
    },
    stat: async (_ws, folderId, rel) => {
      calls.stat++;
      const abs = `${FOLDERS.find((f) => f.id === folderId)!.path}/${rel}`;
      return { exists: files.has(abs), unreadable: opts.unreadable?.[abs] ?? "" };
    },
    byName: async () => {
      calls.byName++;
      return (opts.byName ?? []).map((path) => ({ path }));
    },
  };
  return { deps, calls };
}

describe("path helpers", () => {
  test("normalize and join", () => {
    expect(normalizePath("/a/./b/../c")).toBe("/a/c");
    expect(normalizePath("/../../x")).toBe("/x");
    expect(joinPath("/w/api", "../web/a.ts")).toBe("/w/web/a.ts");
  });
});

describe("resolveFileRef", () => {
  test("absolute inside a folder", async () => {
    const { deps } = fakeDeps({ files: ["/w/web/src/a.ts"] });
    expect(await resolveFileRef({ path: "/w/web/src/a.ts" }, ctx, deps)).toEqual({
      kind: "found",
      target: { workspaceId: "ws", folderId: "web", folderName: "web", rel: "src/a.ts" },
    });
  });
  test("absolute outside every folder is missing", async () => {
    const { deps } = fakeDeps();
    expect(await resolveFileRef({ path: "/elsewhere/x.ts" }, ctx, deps)).toEqual({ kind: "missing" });
  });
  test("absolute prefers the deepest folder", async () => {
    const deps: ResolverDeps = {
      folders: async () => [
        { id: "outer", name: "outer", path: "/w" },
        { id: "inner", name: "inner", path: "/w/api" },
      ],
      stat: async () => ({ exists: true, unreadable: "" }),
      byName: async () => [],
    };
    const r = await resolveFileRef({ path: "/w/api/a.go" }, ctx, deps);
    expect(r.kind === "found" && r.target.folderId).toBe("inner");
  });
  test("project-relative (the line is not part of the resolution)", async () => {
    const { deps } = fakeDeps({ files: ["/w/api/handler.lua"] });
    expect(await resolveFileRef({ path: "handler.lua", line: 27 }, ctx, deps)).toEqual({
      kind: "found",
      target: { workspaceId: "ws", folderId: "api", folderName: "api", rel: "handler.lua" },
    });
  });
  test("folder-relative fallback", async () => {
    const { deps } = fakeDeps({ files: ["/w/web/src/a.ts"] });
    const r = await resolveFileRef({ path: "src/a.ts" }, ctx, deps);
    expect(r.kind === "found" && r.target.folderId).toBe("web");
  });
  test("by name", async () => {
    const { deps } = fakeDeps({
      files: ["/w/api/kong/plugins/x/handler.lua"],
      byName: ["/w/api/kong/plugins/x/handler.lua", "/w/web/static/y.js"],
    });
    const r = await resolveFileRef({ path: "handler.lua" }, ctx, deps);
    expect(r.kind === "found" && r.target.rel).toBe("kong/plugins/x/handler.lua");
  });
  test("ambiguous, sorted by folder then rel", async () => {
    const { deps } = fakeDeps({
      files: ["/w/web/b/handler.lua", "/w/api/z/handler.lua", "/w/api/a/handler.lua"],
      byName: ["/w/web/b/handler.lua", "/w/api/z/handler.lua", "/w/api/a/handler.lua"],
    });
    const r = await resolveFileRef({ path: "handler.lua" }, ctx, deps);
    expect(r.kind).toBe("ambiguous");
    if (r.kind === "ambiguous") {
      expect(r.targets.map((t) => `${t.folderName}:${t.rel}`)).toEqual(["api:a/handler.lua", "api:z/handler.lua", "web:b/handler.lua"]);
    }
  });
  test("by name keeps only paths ending in the reference", async () => {
    const { deps } = fakeDeps({
      files: ["/w/api/x/handler.lua", "/w/api/other/handler.lua"],
      byName: ["/w/api/x/handler.lua", "/w/api/other/handler.lua"],
    });
    const r = await resolveFileRef({ path: "x/handler.lua" }, ctx, deps);
    expect(r).toEqual({
      kind: "found",
      target: { workspaceId: "ws", folderId: "api", folderName: "api", rel: "x/handler.lua" },
    });
  });
  test("missing", async () => {
    const { deps } = fakeDeps();
    expect(await resolveFileRef({ path: "nope.ts" }, ctx, deps)).toEqual({ kind: "missing" });
  });
  test("scratch session is unsupported and calls nothing", async () => {
    const { deps, calls } = fakeDeps();
    expect(await resolveFileRef({ path: "a.ts" }, { ...ctx, workspaceId: "" }, deps)).toEqual({ kind: "unsupported" });
    expect(calls).toEqual({ folders: 0, stat: 0, byName: 0 });
  });
  test("unreadable still counts as found", async () => {
    const { deps } = fakeDeps({ files: ["/w/api/logo.png"], unreadable: { "/w/api/logo.png": "binary" } });
    const r = await resolveFileRef({ path: "logo.png" }, ctx, deps);
    expect(r.kind === "found" && r.target.unreadable).toBe("binary");
  });
  test("leading ../ reaches a sibling folder", async () => {
    const { deps } = fakeDeps({ files: ["/w/web/src/a.ts"] });
    const r = await resolveFileRef({ path: "../web/src/a.ts" }, ctx, deps);
    expect(r.kind === "found" && r.target).toMatchObject({ folderId: "web", rel: "src/a.ts" });
  });
  test("a stat that rejects is not there", async () => {
    const deps: ResolverDeps = {
      folders: async () => FOLDERS,
      stat: async (_w, folderId) => {
        if (folderId === "api") throw new Error("boom");
        return { exists: true, unreadable: "" };
      },
      byName: async () => [],
    };
    const r = await resolveFileRef({ path: "src/a.ts" }, ctx, deps);
    expect(r.kind === "found" && r.target.folderId).toBe("web");
  });
});

describe("line handling and limits", () => {
  test("the core never puts a line in a target", async () => {
    const { deps } = fakeDeps({ files: ["/w/api/a.go"] });
    const r = await resolveFileRef({ path: "a.go", line: 9 }, ctx, deps);
    expect(r.kind === "found" && "line" in r.target).toBe(false);
  });
  test("withLine copies, for found and ambiguous", () => {
    const t = { workspaceId: "ws", folderId: "api", folderName: "api", rel: "a.go" };
    const found = { kind: "found" as const, target: t };
    expect(withLine(found, 3)).toEqual({ kind: "found", target: { ...t, line: 3 } });
    expect(found.target).not.toHaveProperty("line");
    expect(withLine({ kind: "ambiguous", targets: [t, t] }, 4)).toEqual({ kind: "ambiguous", targets: [{ ...t, line: 4 }, { ...t, line: 4 }] });
    expect(withLine(found, undefined)).toBe(found);
    expect(withLine({ kind: "missing" }, 3)).toEqual({ kind: "missing" });
  });
  test("two refs with one path share a query and each gets its own line", () => {
    const c = { sessionId: "s1", workspaceId: "ws", projectPath: "/w/api" };
    expect(fileRefQuery({ path: "a.go", line: 1 }, c).queryKey).toEqual(fileRefQuery({ path: "a.go", line: 2 }, c).queryKey);
  });
  test("a shared resolution gets each line", async () => {
    const { deps } = fakeDeps({ files: ["/w/api/a.go"] });
    const shared = await resolveFileRef({ path: "a.go" }, ctx, deps);
    const a = withLine(shared, 1);
    const b = withLine(shared, 2);
    expect(a.kind === "found" && a.target.line).toBe(1);
    expect(b.kind === "found" && b.target.line).toBe(2);
  });
  test("by name stats at most 11 of 100 matches (and answers ambiguous with 10)", async () => {
    const many = Array.from({ length: 100 }, (_, i) => `/w/api/d${i}/handler.lua`);
    let byNameStats = 0;
    let searched = false;
    const deps: ResolverDeps = {
      folders: async () => FOLDERS,
      stat: async (_w, _f, rel) => {
        if (searched && rel.includes("/")) byNameStats++;
        return { exists: true, unreadable: "" };
      },
      byName: async () => {
        searched = true;
        return many.map((path) => ({ path }));
      },
    };
    // Roots miss (stat only says yes for nested rel paths), then by name.
    const wrapped: ResolverDeps = { ...deps, stat: async (w, f, rel) => (rel.includes("/") ? deps.stat(w, f, rel) : { exists: false, unreadable: "" }) };
    await resolveFileRef({ path: "handler.lua" }, ctx, wrapped);
    expect(byNameStats).toBeLessThanOrEqual(11);
  });
  test("a failing folder lookup throws instead of answering missing", async () => {
    const deps: ResolverDeps = {
      folders: async () => {
        throw new Error("bridge down");
      },
      stat: async () => ({ exists: false, unreadable: "" }),
      byName: async () => [],
    };
    await expect(resolveFileRef({ path: "a.ts" }, ctx, deps)).rejects.toThrow("bridge down");
  });
});

describe("an elided path", () => {
  const elided = { path: "core/.../DispatchFunction.java" };

  test("is found by its file name where the written ends fit", async () => {
    const { deps, calls } = fakeDeps({
      files: ["/w/api/core/src/main/java/io/x/DispatchFunction.java"],
      byName: ["/w/api/core/src/main/java/io/x/DispatchFunction.java", "/w/api/other/src/DispatchFunction.java"],
    });
    expect(await resolveFileRef(elided, ctx, deps)).toEqual({
      kind: "found",
      target: { workspaceId: "ws", folderId: "api", folderName: "api", rel: "core/src/main/java/io/x/DispatchFunction.java" },
    });
    // It was never looked up as the literal path it is not.
    expect(calls.byName).toBe(1);
    expect(calls.stat).toBe(1);
  });

  test("is missing when nothing with that name fits its ends", async () => {
    const { deps } = fakeDeps({ byName: ["/w/api/other/src/DispatchFunction.java"] });
    expect(await resolveFileRef(elided, ctx, deps)).toEqual({ kind: "missing" });
  });

  test("is ambiguous when two files fit, one in each folder", async () => {
    const { deps } = fakeDeps({
      files: ["/w/api/core/a/DispatchFunction.java", "/w/web/core/b/DispatchFunction.java"],
      byName: ["/w/api/core/a/DispatchFunction.java", "/w/web/core/b/DispatchFunction.java"],
    });
    const result = await resolveFileRef(elided, ctx, deps);
    expect(result.kind).toBe("ambiguous");
  });

  test("an elision in the file's own place names nothing", async () => {
    const { deps, calls } = fakeDeps({ byName: ["/w/api/core/x.java"] });
    expect(await resolveFileRef({ path: "core/..." }, ctx, deps)).toEqual({ kind: "missing" });
    expect(calls.byName).toBe(0);
  });

  test("an absolute one keeps its root", async () => {
    const { deps } = fakeDeps({ files: ["/w/api/core/a/X.java"], byName: ["/w/api/core/a/X.java", "/elsewhere/core/a/X.java"] });
    const result = await resolveFileRef({ path: "/w/api/.../X.java" }, ctx, deps);
    expect(result).toEqual({
      kind: "found",
      target: { workspaceId: "ws", folderId: "api", folderName: "api", rel: "core/a/X.java" },
    });
  });
});
