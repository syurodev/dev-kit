import { describe, expect, test } from "bun:test";
import { filterSettingsNavigation, SETTINGS_GROUPS } from "./settingsNavigation";

describe("Settings navigation", () => {
  test("keeps existing deep links and gives security, updates and MCP their own destinations", () => {
    const ids = SETTINGS_GROUPS.flatMap((group) => group.items.map((item) => item.id));
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ["account", "appearance", "sync", "environments", "database", "agents", "keyboard", "security", "updates", "mcp", "remote"]) expect(ids).toContain(id);
  });
  test("finds a section from its contents, including multiple search words", () => {
    expect(filterSettingsNavigation("terminal font").flatMap((group) => group.items.map((item) => item.id))).toEqual(["appearance"]);
    expect(filterSettingsNavigation("credentials").flatMap((group) => group.items.map((item) => item.id))).toEqual(["security"]);
    expect(filterSettingsNavigation("telegram").flatMap((group) => group.items.map((item) => item.id))).toEqual(["remote"]);
    expect(filterSettingsNavigation("phone pairing").flatMap((group) => group.items.map((item) => item.id))).toEqual(["remote"]);
    expect(filterSettingsNavigation("not a setting")).toEqual([]);
    expect(filterSettingsNavigation("  ")).toHaveLength(3);
  });
});
