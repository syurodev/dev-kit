import { describe, expect, it } from "bun:test";
import { nestedRoots, withoutNested, Within } from "./paths";

const f = (id: string, path: string) => ({ id, path });

describe("nested folders", () => {
  const outer = f("o", "/w/orders-service");
  const inner = f("c", "/w/orders-service/libs/common");

  it("names the nested folders of a folder, relative to it", () => {
    expect(nestedRoots(outer, [outer, inner, f("g", "/w/gateway")])).toEqual(["libs/common"]);
    expect(nestedRoots(inner, [outer, inner])).toEqual([]);
  });

  it("does not treat a name that merely starts the same as nested", () => {
    expect(nestedRoots(f("a", "/w/kong-plugin"), [f("b", "/w/kong-plugin-v2")])).toEqual([]);
  });

  it("reads Windows separators", () => {
    expect(nestedRoots(f("a", "C:\\w\\svc"), [f("b", "C:\\w\\svc\\lib")])).toEqual(["lib"]);
  });

  it("leaves a nested folder's files out of the outer listing, and keeps the rest", () => {
    const entries = [{ path: "libs" }, { path: "libs/common" }, { path: "libs/common/a.ts" }, { path: "libs/other/b.ts" }, { path: "src/c.ts" }];
    expect(withoutNested(entries, ["libs/common"]).map((e) => e.path)).toEqual(["libs", "libs/other/b.ts", "src/c.ts"]);
  });

  it("does not leave behind a directory that held only the nested folder", () => {
    const entries = [{ path: "libs" }, { path: "libs/common" }, { path: "libs/common/a.ts" }, { path: "src" }, { path: "src/c.ts" }];
    expect(withoutNested(entries, ["libs/common"]).map((e) => e.path)).toEqual(["src", "src/c.ts"]);
  });

  it("Within agrees on the same rule", () => {
    expect(Within("/w/a/b", "/w/a")).toBe(true);
    expect(Within("/w/ab", "/w/a")).toBe(false);
  });
});
