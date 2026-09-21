import { describe, expect, it } from "bun:test";
import { buildSwitcherItems } from "../workbench/switcherItems";
import { toolsModule } from "./tools/manifest";
import { notesModule } from "./notes/manifest";
import { databaseModule } from "./database/manifest";
import { sshModule } from "./ssh/manifest";
import { kafkaModule } from "./kafka/manifest";
import { agenthubModule } from "./agenthub/manifest";
import { codegraphModule } from "./codegraph/manifest";
import { accountsModule } from "./accounts/manifest";
import { tasksModule } from "./tasks/manifest";
import { gitModule } from "./git/manifest";
import { apiclientModule } from "./apiclient/manifest";
import type { ModuleManifest } from "./registry";

// The modules that have a button of their own or share one; Settings and Sync
// live in the header (Workbench's HEADER_ONLY_MODULE_IDS).
const ON_THE_BAR: ModuleManifest[] = [
  toolsModule,
  notesModule,
  databaseModule,
  sshModule,
  kafkaModule,
  agenthubModule,
  codegraphModule,
  accountsModule,
  tasksModule,
  gitModule,
  apiclientModule,
];

describe("module groups", () => {
  const items = buildSwitcherItems(
    [...ON_THE_BAR].sort((a, b) => a.id.localeCompare(b.id)).map((m) => ({ id: m.id, title: m.title })),
    (id) => ON_THE_BAR.find((m) => m.id === id)?.group,
  );

  it("put the bar in six slots, so every one has a ⌘digit", () => {
    expect(items.map((item) => (item.kind === "module" ? item.module.id : item.group.id))).toEqual([
      "agenthub",
      "codegraph",
      "git",
      "infra",
      "plan",
      "utilities",
    ]);
  });

  it("name a primary that is one of their own members", () => {
    for (const item of items) {
      if (item.kind !== "group" || !item.group.primary) continue;
      expect(item.members.map((m) => m.id)).toContain(item.group.primary);
    }
  });

  it("do not share an id with a module, which would sort the two into one slot", () => {
    const moduleIds = new Set(ON_THE_BAR.map((m) => m.id));
    for (const item of items) {
      if (item.kind === "group") expect(moduleIds.has(item.group.id)).toBe(false);
    }
  });
});
