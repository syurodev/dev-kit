import { describe, expect, test } from "bun:test";
import type { EntityRef, IntentOptions } from "@/workbench/intents";
import { openFileRef, refOpener, type OpenDeps } from "./openReference";
import type { RefResolution, RefTarget } from "./resolveFileRef";

const ctx = { sessionId: "s1", workspaceId: "ws", projectPath: "/w/api" };
const A: RefTarget = { workspaceId: "ws", folderId: "api", folderName: "api", rel: "kong/handler.lua" };
const B: RefTarget = { workspaceId: "ws", folderId: "gw", folderName: "gateway", rel: "plugins/handler.lua" };

function fakeDeps(resolution: RefResolution, picked: RefTarget | null | "unavailable" = null) {
  const calls = {
    dispatch: [] as { name: string; ref: EntityRef; options: IntentOptions }[],
    toast: [] as string[],
    pick: [] as { anchor: HTMLElement | null; targets: RefTarget[] }[],
    resolve: 0,
  };
  const deps: OpenDeps = {
    resolve: async () => {
      calls.resolve++;
      return resolution;
    },
    dispatch: async (name, ref, options = {}) => {
      calls.dispatch.push({ name, ref, options });
      return "ok";
    },
    toastError: (message) => {
      calls.toast.push(message);
    },
    pick: async (anchor, targets) => {
      calls.pick.push({ anchor, targets });
      return picked;
    },
  };
  return { deps, calls };
}

describe("openFileRef", () => {
  test("found opens the file by its ids, beside the session", async () => {
    const { deps, calls } = fakeDeps({ kind: "found", target: { ...A, line: 1 } });
    await openFileRef({ path: "kong/handler.lua", line: 12 }, ctx, {}, deps);
    expect(calls.dispatch).toEqual([
      {
        name: "codegraph.reveal",
        ref: {
          module: "codegraph",
          kind: "file",
          id: "kong/handler.lua",
          label: "handler.lua",
          workspaceId: "ws",
          folderId: "api",
          // The line is the reference's, never a cached resolution's.
          line: 12,
        },
        options: { placement: "beside" },
      },
    ]);
    expect(calls.toast).toEqual([]);
  });

  test("an explicit placement is kept", async () => {
    const { deps, calls } = fakeDeps({ kind: "found", target: A });
    await openFileRef({ path: "kong/handler.lua" }, ctx, { placement: "replace" }, deps);
    expect(calls.dispatch[0].options).toEqual({ placement: "replace" });
    expect(calls.dispatch[0].ref.line).toBeUndefined();
  });

  test("missing says so and opens nothing", async () => {
    const { deps, calls } = fakeDeps({ kind: "missing" });
    await openFileRef({ path: "handler.lua" }, ctx, {}, deps);
    expect(calls.toast).toEqual(["Couldn't find handler.lua in this workspace"]);
    expect(calls.dispatch).toEqual([]);
  });

  test("ambiguous asks which, then opens the one picked", async () => {
    const { deps, calls } = fakeDeps({ kind: "ambiguous", targets: [A, B] }, B);
    const anchor = {} as HTMLElement;
    await openFileRef({ path: "handler.lua", line: 4 }, ctx, { anchor }, deps);
    expect(calls.pick).toEqual([{ anchor, targets: [A, B] }]);
    expect(calls.dispatch).toHaveLength(1);
    expect(calls.dispatch[0].ref).toMatchObject({ id: "plugins/handler.lua", workspaceId: "ws", folderId: "gw", line: 4 });
  });

  test("ambiguous and dismissed opens nothing", async () => {
    const { deps, calls } = fakeDeps({ kind: "ambiguous", targets: [A, B] }, null);
    await openFileRef({ path: "handler.lua" }, ctx, {}, deps);
    expect(calls.pick).toHaveLength(1);
    expect(calls.dispatch).toEqual([]);
    expect(calls.toast).toEqual([]);
  });

  test("unsupported falls back to the project-relative reveal", async () => {
    const { deps, calls } = fakeDeps({ kind: "unsupported" });
    await openFileRef({ path: "src/a.ts", line: 2 }, { ...ctx, workspaceId: "" }, {}, deps);
    expect(calls.dispatch).toEqual([
      {
        name: "codegraph.reveal",
        ref: { module: "codegraph", kind: "file", id: "src/a.ts", label: "a.ts", project: "/w/api", line: 2 },
        options: { placement: "beside" },
      },
    ]);
  });

  test("with no session context nothing is resolved and the old reveal runs", async () => {
    const { deps, calls } = fakeDeps({ kind: "found", target: A });
    await openFileRef({ path: "/w/api/src/a.ts" }, null, {}, deps);
    expect(calls.resolve).toBe(0);
    expect(calls.dispatch[0].ref).toEqual({ module: "codegraph", kind: "file", id: "/w/api/src/a.ts", label: "a.ts", project: "" });
  });

  test("a resolver that throws is reported, not swallowed", async () => {
    const { deps, calls } = fakeDeps({ kind: "missing" });
    deps.resolve = async () => {
      throw new Error("bridge down");
    };
    await openFileRef({ path: "a.ts" }, ctx, {}, deps);
    expect(calls.toast).toEqual(["Couldn't open a.ts: bridge down"]);
    expect(calls.dispatch).toEqual([]);
  });

  test("a reveal that throws is reported", async () => {
    const { deps, calls } = fakeDeps({ kind: "found", target: A });
    deps.dispatch = async () => {
      throw new Error("tab store gone");
    };
    await openFileRef({ path: "kong/handler.lua" }, ctx, {}, deps);
    expect(calls.toast).toEqual(["Couldn't open kong/handler.lua: tab store gone"]);
  });

  test("a project reveal that finds nothing says so", async () => {
    const { deps, calls } = fakeDeps({ kind: "unsupported" });
    deps.dispatch = async () => "unavailable";
    await openFileRef({ path: "src/a.ts" }, { ...ctx, workspaceId: "" }, {}, deps);
    expect(calls.toast).toEqual(["Couldn't find src/a.ts in this workspace"]);
  });

  test("several matches and nowhere to ask says so", async () => {
    const { deps, calls } = fakeDeps({ kind: "ambiguous", targets: [A, B] }, "unavailable");
    await openFileRef({ path: "handler.lua" }, ctx, {}, deps);
    expect(calls.toast).toEqual(["Several files match handler.lua"]);
    expect(calls.dispatch).toEqual([]);
  });
});

describe("refOpener", () => {
  const seen: { placement?: string; anchor?: HTMLElement | null }[] = [];
  const open = async (_ref: unknown, _ctx: unknown, opts: { placement?: string; anchor?: HTMLElement | null }) => {
    seen.push(opts);
  };

  test("the surface's placement is the default, an explicit one wins", () => {
    seen.length = 0;
    const rail = refOpener(ctx, "replace", open);
    rail({ path: "a.ts" }, null);
    rail({ path: "a.ts" }, null, "beside");
    expect(seen.map((o) => o.placement)).toEqual(["replace", "beside"]);
  });
});
