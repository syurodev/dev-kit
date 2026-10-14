import { describe, expect, test } from "bun:test";
import { revealFileArgs, revealFileRef, type RevealFileActions } from "./revealFile";

const base = { module: "codegraph", kind: "file", id: "src/a.ts", label: "a.ts" };

describe("revealFileArgs", () => {
  test("a ref with workspace and folder ids opens that file directly", () => {
    expect(revealFileArgs({ ...base, workspaceId: "w", folderId: "f", line: 3 }, "beside")).toEqual({
      location: { workspaceId: "w", folderId: "f", path: "src/a.ts" },
      line: 3,
      options: { preview: false, beside: true },
    });
  });

  test("no line opens at the top, and replace stays in the group", () => {
    expect(revealFileArgs({ ...base, workspaceId: "w", folderId: "f" }, "replace")).toEqual({
      location: { workspaceId: "w", folderId: "f", path: "src/a.ts" },
      line: null,
      options: { preview: false, beside: false },
    });
  });

  test("without both ids the old project resolution applies", () => {
    expect(revealFileArgs({ ...base, project: "/w/api" }, "beside")).toBeNull();
    expect(revealFileArgs({ ...base, workspaceId: "w" }, "beside")).toBeNull();
    expect(revealFileArgs({ ...base, folderId: "f" }, "beside")).toBeNull();
  });
});

describe("revealFileRef", () => {
  function fakeActions(project: "opened" | "failed" | "not-found") {
    const calls: string[] = [];
    const actions: RevealFileActions = {
      openResolvedFile: async (args) => {
        calls.push(`ids:${args.location.folderId}/${args.location.path}`);
        return true;
      },
      openFileInProject: async (root, path) => {
        calls.push(`project:${root}:${path}`);
        return project;
      },
    };
    return { actions, calls };
  }

  test("ids open directly", async () => {
    const { actions, calls } = fakeActions("not-found");
    expect(await revealFileRef({ ...base, workspaceId: "w", folderId: "f" }, "beside", actions)).toBe("ok");
    expect(calls).toEqual(["ids:f/src/a.ts"]);
  });

  test("a project path no folder holds is unavailable, not a silent no-op", async () => {
    const { actions } = fakeActions("not-found");
    expect(await revealFileRef({ ...base, project: "/w/api" }, "beside", actions)).toBe("unavailable");
  });

  test("a file that opened, even if its load failed in the tab, is ok", async () => {
    expect(await revealFileRef({ ...base, project: "/w/api" }, "beside", fakeActions("opened").actions)).toBe("ok");
    expect(await revealFileRef({ ...base, project: "/w/api" }, "beside", fakeActions("failed").actions)).toBe("ok");
  });
});
