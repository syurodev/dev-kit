import { describe, expect, it } from "bun:test";
import { accountsModule } from "./accounts/manifest";
import { agenthubModule } from "./agenthub/manifest";
import { apiclientModule } from "./apiclient/manifest";
import { codegraphModule } from "./codegraph/manifest";
import { databaseModule } from "./database/manifest";
import { gitModule } from "./git/manifest";
import { kafkaModule } from "./kafka/manifest";
import { notesModule } from "./notes/manifest";
import { settingsModule } from "./settings/manifest";
import { sshModule } from "./ssh/manifest";
import { syncModule } from "./sync/manifest";
import { tasksModule } from "./tasks/manifest";
import { toolsModule } from "./tools/manifest";

// Arriving at a module with nothing open used to show "has nothing open" and
// no rail for every module but Git. A module whose home is a list or a
// dashboard opens it; one whose home is a NEW document (a blank console, a
// blank request) keeps the empty state, since opening it would create one.
describe("which modules open their home on arrival", () => {
  it("list and dashboard homes open", () => {
    for (const m of [accountsModule, agenthubModule, codegraphModule, gitModule, kafkaModule, notesModule,
      settingsModule, sshModule, syncModule, tasksModule, toolsModule]) {
      expect({ id: m.id, opensHome: m.opensHome }).toEqual({ id: m.id, opensHome: true });
    }
  });

  it("homes that are a new document do not", () => {
    for (const m of [databaseModule, apiclientModule]) {
      expect({ id: m.id, opensHome: m.opensHome ?? false }).toEqual({ id: m.id, opensHome: false });
    }
  });
});
