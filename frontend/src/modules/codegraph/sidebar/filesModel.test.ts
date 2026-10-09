import { describe, expect, it } from "bun:test";
import { folderView, hiddenNotice, filtersActive } from "./filesModel";

const e = (path: string, isDir = false) => ({ path, isDir });
const files = [e("src", true), e("src/a.ts"), e("src/b.ts"), e("README.md"), e("pom.xml")];
const none = { codeOnly: false, changed: false, inFlow: false };
const base = { unsupported: false, git: {}, flow: null };

const paths = (v: ReturnType<typeof folderView>) => {
  const out: string[] = [];
  const walk = (bs: typeof v.branches) => bs.forEach((b) => (out.push(b.path), walk(b.children)));
  walk(v.branches);
  return out;
};

describe("folderView", () => {
  it("lists every file with no toggle on", () => {
    const v = folderView("f", files, "", none, base);
    expect(v.shown).toBe(4);
    expect(v.total).toBe(4);
    expect(v.hiddenBy).toEqual([]);
  });

  it("Code only hides config and docs, and says it is the reason", () => {
    const v = folderView("f", files, "", { ...none, codeOnly: true }, base);
    expect(paths(v)).toContain("src/a.ts");
    expect(paths(v)).not.toContain("pom.xml");
    expect(v.hiddenBy).toEqual(["code"]);
    expect(hiddenNotice(v)).toBe("2 files hidden by Code only");
  });

  it("does not apply Code only to a folder no analyzer reads: there, config is the code", () => {
    const v = folderView("f", files, "", { ...none, codeOnly: true }, { ...base, unsupported: true });
    expect(v.shown).toBe(4);
    expect(v.hiddenBy).toEqual([]);
  });

  it("Changed uses this folder's own git state, not another's", () => {
    const v = folderView("f", files, "", { ...none, changed: true }, { ...base, git: { "src/b.ts": "M" } });
    expect(paths(v)).toEqual(["src", "src/b.ts"]);
    expect(folderView("g", files, "", { ...none, changed: true }, base).shown).toBe(0);
  });

  it("In flow only means something in the flow's own folder", () => {
    const flow = { folderId: "f", files: new Set(["src/a.ts"]) };
    expect(folderView("f", files, "", { ...none, inFlow: true }, { ...base, flow }).shown).toBe(1);
    const other = folderView("g", files, "", { ...none, inFlow: true }, { ...base, flow });
    expect(other.shown).toBe(0);
    expect(other.hiddenBy).toEqual(["flow"]);
  });

  it("a search opens the folders holding its matches and reports no filter hiding", () => {
    const v = folderView("f", files, "b.ts", none, base);
    expect(v.forceExpanded?.has("src")).toBe(true);
    expect(v.hiddenBy).toEqual([]);
  });

  it("resets only when something differs from the defaults", () => {
    expect(filtersActive({ codeOnly: true, changed: false, inFlow: false })).toBe(false);
    expect(filtersActive({ codeOnly: true, changed: true, inFlow: false })).toBe(true);
    expect(filtersActive({ codeOnly: false, changed: false, inFlow: false })).toBe(true);
  });
});
