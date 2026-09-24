import { describe, expect, it } from "bun:test";
import { addFolder, folderName, makePrimary, removeFolder, rowsFromServices } from "./workspaceDialogModel";

const svc = (name: string, path: string) => ({ id: name, workspace_id: "w", name, path, name_source: "folder" });

describe("workspace dialog rows", () => {
  it("names a folder by its last segment", () => {
    expect(folderName("/src/fsap-config-service")).toBe("fsap-config-service");
    expect(folderName("/src/kong-plugin/")).toBe("kong-plugin");
    expect(folderName("C:\\\\work\\\\api")).toBe("api");
  });

  it("puts the primary first", () => {
    const rows = rowsFromServices([svc("a", "/a"), svc("b", "/b"), svc("c", "/c")], "/b");
    expect(rows.map((r) => r.path)).toEqual(["/b", "/a", "/c"]);
  });

  it("adds a folder once, and removes it", () => {
    let rows = addFolder([], "/x/api");
    rows = addFolder(rows, "/x/api");
    expect(rows).toEqual([{ path: "/x/api", name: "api" }]);
    expect(removeFolder(rows, "/x/api")).toEqual([]);
  });

  it("moves a row to the top to make it primary", () => {
    const rows = [
      { path: "/a", name: "a" },
      { path: "/b", name: "b" },
    ];
    expect(makePrimary(rows, "/b").map((r) => r.path)).toEqual(["/b", "/a"]);
    expect(makePrimary(rows, "/a")).toBe(rows);
  });
});
