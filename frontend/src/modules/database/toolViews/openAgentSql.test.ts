import { beforeEach, describe, expect, test } from "bun:test";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import type { ConnectionProfile } from "@/bridge/database";
import { resetConsoleHydration } from "../consoleHydration";
import { useDatabaseStore } from "../databaseStore";
import { openSqlConsole } from "../documents";
import { AGENT_QUERY_TITLE, agentSqlNotice, openAgentSql } from "./openAgentSql";

const profile = (id: string, name: string) => ({ id, name }) as ConnectionProfile;

beforeEach(() => {
  resetConsoleHydration();
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {}, groups: [], focusedGroupId: "" });
  useDatabaseStore.setState({
    profiles: [profile("p1", "staging"), profile("p2", "prod")],
    selectedProfileId: "p1",
    consoles: {},
  });
});

describe("Open in Database from an agent's result", () => {
  test("opens a console titled Agent query beside the transcript", async () => {
    const calls: unknown[][] = [];
    await openAgentSql("select 1", "p1", async (...args) => {
      calls.push(args);
    });
    expect(calls).toEqual([["select 1", AGENT_QUERY_TITLE, { beside: true }]]);
    expect(AGENT_QUERY_TITLE).toBe("Agent query");
  });

  test("opens, never runs", async () => {
    // Wrapped through setState, not spyOn: zustand copies the state object on
    // every set, so a spy on one copy outlives its restore in the next.
    const original = useDatabaseStore.getState().executeQuery;
    let runs = 0;
    useDatabaseStore.setState({ executeQuery: (...args) => (runs++, original(...args)) });
    try {
      await openAgentSql("delete from users", "p1", (sql, title, options) =>
        openSqlConsole(sql, title, { ...options, load: async () => [] }),
      );
    } finally {
      useDatabaseStore.setState({ executeQuery: original });
    }
    expect(runs).toBe(0);
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
    expect(tabs.openTabs.map((tab) => tab.title)).toEqual(["Agent query"]);
    const consoleId = tabs.openTabs[0].params?.consoleId as string;
    expect(useDatabaseStore.getState().consoleState(consoleId).sql).toBe("delete from users");
  });
});

describe("what it says about the connection", () => {
  const state = () => useDatabaseStore.getState();

  test("nothing when the agent used the connection the console is on", () => {
    expect(agentSqlNotice("p1", state())).toBeNull();
  });

  test("names both connections when they differ", () => {
    expect(agentSqlNotice("p2", state())).toBe("The agent used prod; this console is on staging.");
  });

  test("a connection this window does not know is another connection", () => {
    expect(agentSqlNotice("gone", state())).toBe("The agent used another connection; this console is on staging.");
  });

  test("no connection selected says so", () => {
    useDatabaseStore.setState({ selectedProfileId: "" });
    expect(agentSqlNotice("p2", state())).toBe("The agent used prod; this console is on no connection.");
  });

  test("a result that does not name its connection says that", () => {
    expect(agentSqlNotice(undefined, state())).toBe(
      "This result does not say which connection the agent used; this console is on staging.",
    );
  });
});
