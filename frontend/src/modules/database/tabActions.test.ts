import { describe, expect, test } from "bun:test";
import { consoleFilePath, databaseTabActions } from "./tabActions";

describe("a console's file path", () => {
  test("joins with the folder's own separator", () => {
    expect(consoleFilePath("/Users/me/consoles", "console-1")).toBe("/Users/me/consoles/console-1.sql");
    expect(consoleFilePath("C:\\Users\\me\\consoles", "console-1")).toBe("C:\\Users\\me\\consoles\\console-1.sql");
  });

  test("a trailing separator is not doubled", () => {
    expect(consoleFilePath("/tmp/consoles/", "c")).toBe("/tmp/consoles/c.sql");
  });
});

describe("what a Database tab adds to its menu", () => {
  const tab = (viewId: string, params?: Record<string, unknown>) => ({
    id: `database:${viewId}`,
    moduleId: "database",
    viewId,
    title: "t",
    params,
  });

  test("a table offers its names, its DDL and a console", () => {
    const labels = databaseTabActions(tab("database.table", { schema: "public", table: "orders" })).map(
      (action) => ("label" in action ? action.label : "—"),
    );
    expect(labels).toEqual(["Copy", "Show DDL", "Open Query Console"]);
  });

  test("an empty console cannot be saved or have its SQL copied", () => {
    const actions = databaseTabActions(tab("database.console", { consoleId: "console-empty" }));
    const save = actions.find((action) => action.id === "save");
    expect(save && save.type === "item" && save.disabled).toBe(true);
  });

  test("views with nothing to add add nothing", () => {
    expect(databaseTabActions(tab("database.erd"))).toEqual([]);
  });
});
