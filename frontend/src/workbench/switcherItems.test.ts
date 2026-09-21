import { describe, expect, test } from "bun:test";
import { INFRA_GROUP, PLAN_GROUP, UTILITIES_GROUP } from "@/modules/groups";
import { buildSwitcherItems, representative, slotModuleIds, slotTarget, type SwitcherModule } from "./switcherItems";

const mod = (id: string, unavailable = false): SwitcherModule => ({ id, title: id, unavailable });
const infra = (id: string) => (["database", "kafka", "ssh"].includes(id) ? INFRA_GROUP : undefined);
const everyGroup = (id: string) =>
  ({
    database: INFRA_GROUP,
    kafka: INFRA_GROUP,
    ssh: INFRA_GROUP,
    notes: PLAN_GROUP,
    tasks: PLAN_GROUP,
    accounts: UTILITIES_GROUP,
    apiclient: UTILITIES_GROUP,
    tools: UTILITIES_GROUP,
  })[id];
const registry = ["accounts", "agenthub", "apiclient", "codegraph", "database", "git", "kafka", "notes", "ssh", "tasks", "tools"];

describe("buildSwitcherItems", () => {
  const items = buildSwitcherItems(registry.map((id) => mod(id)), infra);

  test("a group takes one slot, where its own id falls in the order", () => {
    expect(items).toHaveLength(9);
    expect(items.map((item) => (item.kind === "module" ? item.module.id : item.group.id))).toEqual([
      "accounts",
      "agenthub",
      "apiclient",
      "codegraph",
      "git",
      "infra",
      "notes",
      "tasks",
      "tools",
    ]);
  });

  test("its members keep their own order", () => {
    const group = items.find((item) => item.kind === "group");
    expect(group?.kind === "group" && group.members.map((m) => m.id)).toEqual(["database", "kafka", "ssh"]);
  });

  test("with every group in place the bar is six slots, and rarely used things come last", () => {
    const all = buildSwitcherItems(registry.map((id) => mod(id)), everyGroup);
    expect(all.map((item) => (item.kind === "module" ? item.module.id : item.group.id))).toEqual([
      "agenthub",
      "codegraph",
      "git",
      "infra",
      "plan",
      "utilities",
    ]);
  });

  test("a group with one member is that member", () => {
    const solo = buildSwitcherItems([mod("database"), mod("git")], infra);
    expect(solo.map((item) => item.kind)).toEqual(["module", "module"]);
  });
});

describe("representative", () => {
  const members = [mod("database"), mod("kafka"), mod("ssh")];

  test("is the member on screen, then the one used last, then the first", () => {
    expect(representative(members, "ssh", "kafka")?.id).toBe("kafka");
    expect(representative(members, "ssh", "notes")?.id).toBe("ssh");
    expect(representative(members, undefined, "notes")?.id).toBe("database");
  });

  test("falls back to the group's primary before the first member", () => {
    expect(representative(members, undefined, "notes", "kafka")?.id).toBe("kafka");
    // ...but never over a member that is on screen or was used last.
    expect(representative(members, "ssh", "notes", "kafka")?.id).toBe("ssh");
  });

  test("skips a member that cannot be opened", () => {
    expect(representative([mod("database", true), mod("kafka")], "database", "notes")?.id).toBe("kafka");
    expect(representative([mod("database", true)], undefined, "notes")).toBeNull();
  });
});

describe("slotTarget", () => {
  const items = buildSwitcherItems(registry.map((id) => mod(id)), infra);
  const group = items[5];

  test("a module opens itself", () => {
    expect(slotTarget(items[4], undefined, "notes")?.id).toBe("git");
  });

  test("a group opens the member used last", () => {
    expect(slotTarget(group, "kafka", "notes")?.id).toBe("kafka");
    expect(slotTarget(group, undefined, "notes")?.id).toBe("database");
  });

  test("pressed again from inside the group, moves to the next member and wraps", () => {
    expect(slotTarget(group, "database", "database")?.id).toBe("kafka");
    expect(slotTarget(group, "ssh", "ssh")?.id).toBe("database");
  });

  test("a slot with nothing there, or nothing usable, opens nothing", () => {
    expect(slotTarget(undefined, undefined, "notes")).toBeNull();
    expect(slotTarget({ kind: "module", module: mod("git", true) }, undefined, "notes")).toBeNull();
  });
});

describe("slotModuleIds", () => {
  test("names a module's slot and leaves a group's members without one", () => {
    const ids = slotModuleIds(buildSwitcherItems(registry.map((id) => mod(id)), infra));
    expect(ids.indexOf("git")).toBe(4);
    expect(ids.indexOf("kafka")).toBe(-1);
    expect(ids[5]).toBe("");
  });
});
