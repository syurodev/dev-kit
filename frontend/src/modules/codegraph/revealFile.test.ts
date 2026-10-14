import { describe, expect, test } from "bun:test";
import { revealFileArgs } from "./revealFile";

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
