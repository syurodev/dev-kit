import { describe, expect, it } from "bun:test";
import { Table2 } from "lucide-react";
import { persistedTabs, restoreIcons, type ModuleTabs } from "./workbenchTabs";
import { databaseModule } from "@/modules/database/manifest";

/**
 * The manifest's own answer, without registering it: the registry is global
 * and process-wide, and Workbench registers every module on import, so a test
 * that registered one would make that throw.
 */
const resolve = (moduleId: string, viewId: string) =>
  moduleId === "database"
    ? databaseModule.views.find((view) => view.id === viewId)?.icon
    : undefined;

const tabs: Record<string, ModuleTabs> = {
  database: {
    activeTabId: "database:database.table:schema=chiro&table=ba_user",
    tabMru: ["database:database.table:schema=chiro&table=ba_user"],
    openTabs: [
      {
        id: "database:database.table:schema=chiro&table=ba_user",
        moduleId: "database",
        viewId: "database.table",
        title: "ba_user",
        params: { schema: "chiro", table: "ba_user" },
        icon: Table2,
        status: "running",
        preview: false,
      },
    ],
  },
};

describe("what survives a relaunch", () => {
  // An icon is a function. Storage drops it, which would be harmless if the
  // tab came back without one — it is not: TabStrip falls back to the MODULE's
  // glyph, so every restored table tab would wear a database icon.
  it("stores no functions and restores the glyph from the view id", () => {
    const saved = persistedTabs({ activeModuleId: "database", modules: tabs });
    const stored = saved.modules.database.openTabs[0] as Record<string, unknown>;
    expect(stored).not.toHaveProperty("icon");
    expect(JSON.parse(JSON.stringify(saved))).toEqual(saved);

    const back = restoreIcons(saved.modules as unknown as Record<string, ModuleTabs>, resolve);
    expect(back.database.openTabs[0].icon).toBe(Table2);
  });

  // "Running" belonged to a query that died with the process. Restoring it
  // would spin a tab nothing is working on.
  it("drops a status that died with the app", () => {
    const saved = persistedTabs({ activeModuleId: "database", modules: tabs });
    expect(saved.modules.database.openTabs[0]).not.toHaveProperty("status");
    const back = restoreIcons(saved.modules as unknown as Record<string, ModuleTabs>, resolve);
    expect(back.database.openTabs[0].status).toBeUndefined();
  });

  it("keeps the params that say which document a tab is", () => {
    const saved = persistedTabs({ activeModuleId: "database", modules: tabs });
    expect(saved.modules.database.openTabs[0].params).toEqual({
      schema: "chiro",
      table: "ba_user",
    });
    expect(saved.modules.database.activeTabId).toBe(tabs.database.activeTabId);
    expect(saved.activeModuleId).toBe("database");
  });

  // Nothing stored is a first launch, and a first launch opens nothing.
  it("comes back empty from nothing", () => {
    expect(restoreIcons({}, resolve)).toEqual({});
  });

  // A view the build no longer has still round-trips: the glyph resolves to
  // undefined and the strip falls back, rather than the restore throwing.
  it("survives a tab whose view is gone", () => {
    const stale = restoreIcons(
      {
        ghost: {
          activeTabId: "",
          tabMru: [],
          openTabs: [{ id: "ghost:x", moduleId: "ghost", viewId: "x", title: "Gone" }],
        },
      },
      resolve,
    );
    expect(stale.ghost.openTabs[0].icon).toBeUndefined();
  });
});
