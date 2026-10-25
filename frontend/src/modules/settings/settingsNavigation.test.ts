import { beforeEach, describe, expect, test } from "bun:test";
import { filterSettingsNavigation, resolveSettingsTab, SETTINGS_GROUPS } from "./settingsNavigation";
import { AGENT_VENDOR_KEY, readAgentVendor, writeAgentVendor } from "./agentVendorChoice";

const OFFERED = ["claude", "codex"];
const idsOf = (query: string) => filterSettingsNavigation(query).flatMap((group) => group.items.map((item) => item.id));

describe("Settings navigation", () => {
  test("has unique pages and keeps the non-agent deep links", () => {
    const ids = SETTINGS_GROUPS.flatMap((group) => group.items.map((item) => item.id));
    expect(new Set(ids).size).toBe(ids.length);
    for (const id of ["account", "appearance", "sync", "environments", "database", "keyboard", "security", "updates"]) expect(ids).toContain(id);
  });
  test("Agents is its own group of seven pages, and Services no longer holds agent settings", () => {
    const agents = SETTINGS_GROUPS.find((group) => group.label === "Agents")!;
    expect(agents.items.map((item) => item.id)).toEqual([
      "agents-runtimes", "agents-models", "agents-accounts", "agents-skills", "agents-safety", "agents-notifications", "agents-mcp",
    ]);
    expect(SETTINGS_GROUPS.find((group) => group.label === "Services")!.items.map((item) => item.id)).toEqual(["account", "sync"]);
  });
  test("the open-source notices are a Personal page found by what people call them", () => {
    expect(SETTINGS_GROUPS.find((group) => group.label === "Personal")!.items.map((item) => item.id)).toContain("licenses");
    expect(idsOf("third-party")).toEqual(["licenses"]);
    expect(idsOf("open source")).toEqual(["licenses"]);
    expect(resolveSettingsTab("licenses")).toBe("licenses");
  });
  test("old ids still open the page that took their content over", () => {
    expect(resolveSettingsTab("agents")).toBe("agents-runtimes");
    expect(resolveSettingsTab("remote")).toBe("agents-notifications");
    expect(resolveSettingsTab("mcp")).toBe("agents-mcp");
    expect(resolveSettingsTab("appearance")).toBe("appearance");
    expect(resolveSettingsTab("agents-safety")).toBe("agents-safety");
  });
  test("finds a section from its contents, including multiple search words", () => {
    expect(idsOf("terminal font")).toEqual(["appearance"]);
    expect(idsOf("credentials")).toEqual(["security"]);
    expect(idsOf("telegram")).toEqual(["agents-notifications"]);
    expect(idsOf("phone pairing")).toEqual(["agents-notifications"]);
    expect(idsOf("not a setting")).toEqual([]);
    expect(filterSettingsNavigation("  ")).toHaveLength(4);
  });
  test("an agent topic lands on the one page that holds it", () => {
    expect(idsOf("sandbox")).toEqual(["agents-safety"]);
    expect(idsOf("checkpoints")).toEqual(["agents-safety"]);
    expect(idsOf("subagent")).toEqual(["agents-models"]);
    expect(idsOf("api key")).toEqual(["agents-runtimes"]);
    expect(idsOf("skills")).toEqual(["agents-skills"]);
    expect(idsOf("proxy install")).toEqual(["agents-mcp"]);
    expect(idsOf("notifications")).toEqual(["agents-notifications"]);
    expect(idsOf("scoped approvals")).toEqual(["agents-safety"]);
    expect(idsOf("agents")).toHaveLength(7);
  });
  test("accounts and their limits are found by what people call them", () => {
    expect(idsOf("failover")).toEqual(["agents-accounts"]);
    expect(idsOf("threshold")).toEqual(["agents-accounts"]);
    expect(idsOf("subscription")).toEqual(["agents-accounts"]);
    expect(idsOf("quota")).toEqual(["agents-accounts"]);
    expect(idsOf("accounts limits")).toEqual(["agents-accounts"]);
    expect(resolveSettingsTab("agents-accounts")).toBe("agents-accounts");
  });
});

describe("Agent vendor switch memory", () => {
  const store = new Map<string, string>();
  beforeEach(() => {
    store.clear();
    (globalThis as { localStorage?: unknown }).localStorage = {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => void store.set(key, value),
    };
  });
  test("starts on Claude, remembers the last vendor and ignores junk", () => {
    expect(readAgentVendor(OFFERED)).toBe("claude");
    writeAgentVendor("codex");
    expect(store.get(AGENT_VENDOR_KEY)).toBe("codex");
    expect(readAgentVendor(OFFERED)).toBe("codex");
    store.set(AGENT_VENDOR_KEY, "gemini");
    expect(readAgentVendor(OFFERED)).toBe("claude");
  });
  test("works when storage throws", () => {
    (globalThis as { localStorage?: unknown }).localStorage = {
      getItem: () => { throw new Error("blocked"); },
      setItem: () => { throw new Error("blocked"); },
    };
    expect(readAgentVendor(OFFERED)).toBe("claude");
    expect(() => writeAgentVendor("codex")).not.toThrow();
  });
});
