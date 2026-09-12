import { describe, expect, it } from "bun:test";
import { dropSeededFolders, migrateFoldersToIds } from "./databaseStore";

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

describe("dropSeededFolders", () => {
  it("removes the empty Production and Development the app used to ship with", () => {
    expect(
      dropSeededFolders([
        { id: "production", name: "Production", icon: "server", profileIds: [] },
        { id: "development", name: "Development", icon: "laptop", profileIds: [] },
        { id: "mine", name: "Mine", icon: "folder", profileIds: [] },
      ]),
    ).toEqual([{ id: "mine", name: "Mine", icon: "folder", profileIds: [] }]);
  });

  /**
   * The state that was actually on disk after the v2 pass, read out of
   * devkit.db: uuids, not the ids DEFAULT_FOLDERS gave them, and the "folder"
   * icon migrateFoldersToIds assigns. v2 matched on id and sailed straight
   * past these, which is why they came back.
   */
  it("removes them when an earlier migration rekeyed them onto uuids", () => {
    expect(
      dropSeededFolders([
        {
          id: "2ed35103-071d-43a6-9f94-ae17c9b53dfd",
          name: "Production",
          icon: "folder",
          profileIds: [],
        },
        {
          id: "ea8a4913-7b07-4c9c-8c5c-0bd89848bad6",
          name: "Development",
          icon: "folder",
          profileIds: [],
        },
      ]),
    ).toEqual([]);
  });

  // A seeded folder someone filled is no longer a default — dropping it would
  // scatter the connections inside back to Ungrouped without saying so.
  it("keeps a seeded folder that has connections in it", () => {
    const used = [
      { id: "production", name: "Production", icon: "server", profileIds: ["p1"] },
      { id: "b0c1", name: "Development", icon: "folder", profileIds: ["p2"] },
    ];
    expect(dropSeededFolders(used)).toEqual(used);
  });

  // The name has to match exactly: someone's "Production DB" is their own.
  it("leaves a folder whose name merely resembles one", () => {
    const mine = [
      { id: "x", name: "Production DB", icon: "folder", profileIds: [] },
      { id: "y", name: "production", icon: "folder", profileIds: [] },
    ];
    expect(dropSeededFolders(mine)).toEqual(mine);
  });
});

/**
 * The whole migration over the exact JSON devkit.db held, which is the only
 * check that answers "will this actually clear them".
 */
describe("migrating the state that was really stored", () => {
  const stored = {
    folders: [
      {
        id: "2ed35103-071d-43a6-9f94-ae17c9b53dfd",
        name: "Production",
        icon: "folder",
        profileIds: [],
      },
      {
        id: "ea8a4913-7b07-4c9c-8c5c-0bd89848bad6",
        name: "Development",
        icon: "folder",
        profileIds: [],
      },
    ],
    expandedFolders: {
      "2ed35103-071d-43a6-9f94-ae17c9b53dfd": false,
      "ea8a4913-7b07-4c9c-8c5c-0bd89848bad6": false,
    },
    browsePageSize: 200,
  };

  it("ends with no folders and no fold state left pointing at them", () => {
    const migrated = migrateFoldersToIds(stored);
    const folders = dropSeededFolders(migrated.folders);
    const kept = new Set(folders.map((folder) => folder.id));
    const expandedFolders = Object.fromEntries(
      Object.entries(migrated.expandedFolders).filter(([id]) => kept.has(id)),
    );

    expect(folders).toEqual([]);
    // The fold map is keyed by folder id: an entry for a folder that no longer
    // exists would sit there forever.
    expect(expandedFolders).toEqual({});
    expect(migrated.browsePageSize).toBe(200);
  });
});
