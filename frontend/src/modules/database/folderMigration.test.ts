import { describe, expect, it } from "bun:test";
import { migrateFoldersToIds } from "./databaseStore";

function ids() {
  let n = 0;
  return () => `id-${++n}`;
}

describe("migrateFoldersToIds", () => {
  it("rekeys folders and their expanded state onto ids, keeping names as labels", () => {
    const out = migrateFoldersToIds(
      {
        folders: { Production: ["p1", "p2"], Development: [] },
        expandedFolders: { Production: false, Development: true },
      },
      ids(),
    );

    expect(out.folders).toEqual([
      { id: "id-1", name: "Production", icon: "folder", profileIds: ["p1", "p2"] },
      { id: "id-2", name: "Development", icon: "folder", profileIds: [] },
    ]);
    expect(out.expandedFolders).toEqual({ "id-1": false, "id-2": true });
  });

  // The whole point of the migration is that nothing is lost on the way. A
  // profile dropped here disappears from its folder silently — the user sees
  // an empty group and no error.
  it("keeps every profile assignment", () => {
    const out = migrateFoldersToIds(
      { folders: { A: ["p1"], B: ["p2", "p3"] }, expandedFolders: {} },
      ids(),
    );

    expect(out.folders.flatMap((f) => f.profileIds).sort()).toEqual(["p1", "p2", "p3"]);
  });

  it("leaves an already-migrated state alone", () => {
    const already = {
      folders: [{ id: "keep", name: "Prod", icon: "server", profileIds: ["p1"] }],
      expandedFolders: { keep: true },
    };

    expect(migrateFoldersToIds(already, ids())).toEqual(already);
  });

  it("survives a store that never had folders", () => {
    const out = migrateFoldersToIds({}, ids());
    expect(out.folders).toEqual([]);
    expect(out.expandedFolders).toEqual({});
  });
});
