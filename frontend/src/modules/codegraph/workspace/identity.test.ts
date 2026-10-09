import { describe, expect, it } from "bun:test";
import { docKey, entryKey, flowKey, folderLabels, normalizeRelPath, parseDocKey, treeFileId } from "./identity";

describe("document identity", () => {
  it("is workspace + folder + normalized path, never the path alone", () => {
    const a = docKey("w1", "fa", "src/config.ts");
    const b = docKey("w1", "fb", "src/config.ts");
    const c = docKey("w2", "fa", "src/config.ts");
    expect(new Set([a, b, c]).size).toBe(3);
  });

  it("normalizes separators and dots but never case", () => {
    expect(normalizeRelPath("./src//app\\main.ts")).toBe("src/app/main.ts");
    expect(normalizeRelPath("/src/./x.ts")).toBe("src/x.ts");
    expect(normalizeRelPath("Src/Main.ts")).toBe("Src/Main.ts");
    expect(docKey("w", "f", "A.ts")).not.toBe(docKey("w", "f", "a.ts"));
  });

  it("refuses to name a file outside its folder", () => {
    expect(normalizeRelPath("../secret")).toBe("");
    expect(normalizeRelPath("a/../../b")).toBe("");
  });

  it("round-trips through parseDocKey, including a path with colons", () => {
    const key = docKey("0123456789abcdef01234567", "fedcba9876543210fedcba98", "dir/a:b.ts");
    expect(parseDocKey(key)).toEqual({
      workspaceId: "0123456789abcdef01234567",
      folderId: "fedcba9876543210fedcba98",
      path: "dir/a:b.ts",
    });
    expect(parseDocKey("nonsense")).toBeNull();
    expect(parseDocKey("a:b")).toBeNull();
  });

  it("keys flows, entry points and tree rows by their owner folder", () => {
    expect(flowKey("f1", "n")).not.toBe(flowKey("f2", "n"));
    expect(entryKey("f1", "ep")).not.toBe(entryKey("f2", "ep"));
    expect(treeFileId("f1", "a.ts")).not.toBe(treeFileId("f2", "a.ts"));
  });
});

describe("folderLabels", () => {
  const f = (id: string, name: string, path: string) => ({ id, name, path });

  it("leaves a unique name alone", () => {
    expect(folderLabels([f("1", "api", "/w/api"), f("2", "web", "/w/web")])).toEqual({ "1": "api", "2": "web" });
  });

  it("tells two folders of the same name apart by their parents", () => {
    const labels = folderLabels([f("1", "api", "/srv/v1/api"), f("2", "api", "/srv/v2/api"), f("3", "web", "/srv/web")]);
    expect(labels["1"]).not.toBe(labels["2"]);
    expect(labels["1"]).toContain("v1");
    expect(labels["2"]).toContain("v2");
    expect(labels["3"]).toBe("web");
  });
});
