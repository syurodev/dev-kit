import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { toolsModule } from "./tools/manifest";
import { notesModule } from "./notes/manifest";
import { databaseModule } from "./database/manifest";
import { sshModule } from "./ssh/manifest";
import { kafkaModule } from "./kafka/manifest";
import { agenthubModule } from "./agenthub/manifest";
import { syncModule } from "./sync/manifest";
import { settingsModule } from "./settings/manifest";
import { codegraphModule } from "./codegraph/manifest";
import { accountsModule } from "./accounts/manifest";
import { tasksModule } from "./tasks/manifest";
import { gitModule } from "./git/manifest";
import { apiclientModule } from "./apiclient/manifest";

/**
 * Go declares each built-in module's views too (internal/app/app.go), and the
 * shell compares that list with the manifest on every launch. A view added on
 * one side only is the "Module metadata is unavailable or out of sync" banner
 * over every tab of the module — which is how Code Graph's document views
 * were found missing from the Go list.
 */
const SOURCE = readFileSync(join(import.meta.dir, "../../../internal/app/app.go"), "utf8");

function goViews(): Map<string, string[]> {
  const out = new Map<string, string[]>();
  // One module literal per chunk; a module whose views come from a function
  // (Tools: tools.CommandIDs()) has no string list and is left out.
  for (const chunk of SOURCE.split(/\n\t\t\{\n/)) {
    const id = chunk.match(/^\s*ID:\s*"([^"]+)"/m)?.[1];
    const views = chunk.match(/Views:\s*\[\]string\{([^}]*)\}/)?.[1];
    if (id && views !== undefined) out.set(id, [...views.matchAll(/"([^"]+)"/g)].map((v) => v[1]));
  }
  return out;
}

function goCommands(): Map<string, string[]> {
  const out = new Map<string, string[]>();
  for (const chunk of SOURCE.split(/\n\t\t\{\n/)) {
    const id = chunk.match(/^\s*ID:\s*"([^"]+)"/m)?.[1];
    // Only a literal []string{...} list: a module whose commands come from a function is compared elsewhere.
    const commands = chunk.match(/Commands:\s*\[\]string\{([^}]*)\}/)?.[1];
    if (id && commands !== undefined) out.set(id, [...commands.matchAll(/"([^"]+)"/g)].map((v) => v[1]));
  }
  return out;
}

const MANIFESTS = [
  toolsModule,
  notesModule,
  databaseModule,
  sshModule,
  kafkaModule,
  agenthubModule,
  syncModule,
  accountsModule,
  tasksModule,
  gitModule,
  apiclientModule,
  settingsModule,
  codegraphModule,
];

describe("module views", () => {
  const go = goViews();

  it("reads the Go list at all", () => {
    expect(go.size).toBeGreaterThan(3);
  });

  for (const manifest of MANIFESTS) {
    it(`${manifest.id}: Go and the manifest name the same views`, () => {
      const declared = go.get(manifest.id);
      // A module Go registers some other way is not this test's business.
      if (!declared) return;
      expect(manifest.views.map((v) => v.id).sort()).toEqual(declared.slice().sort());
    });
  }

  const goCmds = goCommands();
  it("reads the Go command lists at all", () => {
    expect(goCmds.get("codegraph")?.length ?? 0).toBeGreaterThan(0);
  });

  // The shell disables a module whose manifest and Go lists differ, so a renamed command is a dead module.
  for (const manifest of MANIFESTS) {
    it(`${manifest.id}: Go and the manifest name the same commands`, () => {
      const declared = goCmds.get(manifest.id);
      if (!declared) return;
      expect(manifest.commands.map((c) => c.id).sort()).toEqual(declared.slice().sort());
    });
  }
});
