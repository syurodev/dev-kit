import { describe, expect, test } from "bun:test";
import { joinPath, normalizePath, resolveFileRef, type ResolverDeps } from "./resolveFileRef";

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
  test("project-relative with a line", async () => {
    const { deps } = fakeDeps({ files: ["/w/api/handler.lua"] });
    expect(await resolveFileRef({ path: "handler.lua", line: 27 }, ctx, deps)).toEqual({
      kind: "found",
      target: { workspaceId: "ws", folderId: "api", folderName: "api", rel: "handler.lua", line: 27 },
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
