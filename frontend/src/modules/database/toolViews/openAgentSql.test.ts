import { beforeEach, describe, expect, test } from "bun:test";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import type { ConnectionProfile } from "@/bridge/database";
import { resetConsoleHydration } from "../consoleHydration";
import { useDatabaseStore } from "../databaseStore";
import { openSqlConsole } from "../documents";
import { agentResultNote } from "../consoleStatus";
import { AGENT_QUERY_TITLE, agentSqlNotice, connectionPhrase, openAgentSql } from "./openAgentSql";
import type { ProfileSelection } from "./reveal";
import { liveProfileSelection } from "./liveProfiles";

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
    await openAgentSql("select 1", "p1", {
      open: async (...args) => {
        calls.push(args);
      },
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
      await openAgentSql("delete from users", "p1", {
        open: (sql, title, options) => openSqlConsole(sql, title, { ...options, load: async () => [] }),
      });
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

/**
 * A db.query result names the connection that ran it, as db.explain's input
 * does: Open in Database switches to it — the rail's own selection — before
 * the console opens, so the console is on the agent's connection. One that
 * is not on this device opens nothing.
 */
describe("on the agent's connection", () => {
  const fake = (known: string[], selected: string, later: string[] = known) => {
    let ids = known;
    let current = selected;
    const log: string[] = [];
    const profiles: ProfileSelection = {
      profileIds: () => ids,
      selectedProfileId: () => current,
      refreshProfiles: async () => {
        log.push("refresh");
        ids = later;
      },
      selectProfile: (id) => {
        log.push(`select ${id}`);
        current = id;
      },
      switchCost: () => null,
      confirm: async () => (log.push("ask"), true),
    };
    const open = async (sql: string, _title?: string, options?: { result?: unknown }) => {
      log.push(`open ${sql} on ${current}${options?.result ? " with rows" : ""}`);
    };
    return { profiles, open, log };
  };
  const rows = { columns: ["id"], rows: [["7"]], rowCount: 1, truncated: false, rowsAffected: -1 };

  test("switches to it, then opens the statement with the agent's rows", async () => {
    const { profiles, open, log } = fake(["p1", "p2"], "p1");
    const outcome = await openAgentSql("select 1", "p2", { profiles, open, result: rows });
    expect(log).toEqual(["select p2", "open select 1 on p2 with rows"]);
    expect(outcome).toEqual({ opened: true, notice: null });
  });

  test("already on it, nothing is selected again", async () => {
    const { profiles, open, log } = fake(["p1", "p2"], "p2");
    await openAgentSql("select 1", "p2", { profiles, open });
    expect(log).toEqual(["open select 1 on p2"]);
  });

  test("a list not loaded yet is asked for once", async () => {
    const { profiles, open, log } = fake([], "", ["p3"]);
    expect((await openAgentSql("select 1", "p3", { profiles, open })).opened).toBe(true);
    expect(log).toEqual(["refresh", "select p3", "open select 1 on p3"]);
  });

  test("a connection not on this device opens nothing", async () => {
    const { profiles, open, log } = fake(["p1"], "p1");
    const outcome = await openAgentSql("select 1", "elsewhere", { profiles, open, result: rows });
    expect(outcome.opened).toBe(false);
    expect(log).toEqual(["refresh"]);
  });

  test("a selection that did not take opens nothing", async () => {
    const { profiles, open, log } = fake(["p1", "p2"], "p1");
    profiles.selectProfile = (id) => void log.push(`select ${id} (refused)`);
    expect((await openAgentSql("select 1", "p2", { profiles, open })).opened).toBe(false);
    expect(log).toEqual(["select p2 (refused)"]);
  });

  test("asked before losing a transaction, Cancel opens nothing and says nothing more", async () => {
    const { profiles, open, log } = fake(["p1", "p2"], "p1");
    profiles.switchCost = () => ({ from: "staging", to: "prod", transaction: true, runningQueries: 0 });
    profiles.confirm = async () => (log.push("ask"), false);
    expect(await openAgentSql("select 1", "p2", { profiles, open, result: rows })).toEqual({ opened: false, notice: null });
    expect(log).toEqual(["ask"]);
  });

  test("a result that names none opens on the selected connection, says so, and still brings its rows", async () => {
    const { profiles, open, log } = fake(["p1", "p2"], "p1");
    const outcome = await openAgentSql("select 1", undefined, { profiles, open, result: rows });
    expect(log).toEqual(["open select 1 on p1 with rows"]);
    expect(outcome).toEqual({
      opened: true,
      notice: "This result does not say which connection the agent used; this console is on staging.",
    });
  });

  test("through the real store: switched, opened, the rows shown, nothing run", async () => {
    const original = useDatabaseStore.getState().executeQuery;
    let runs = 0;
    useDatabaseStore.setState({ executeQuery: (...args) => (runs++, original(...args)) });
    try {
      await openAgentSql("select id from t", "p2", {
        result: rows,
        open: (sql, title, options) => openSqlConsole(sql, title, { ...options, load: async () => [] }),
      });
    } finally {
      useDatabaseStore.setState({ executeQuery: original });
    }
    expect(runs).toBe(0);
    expect(useDatabaseStore.getState().selectedProfileId).toBe("p2");
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
    const consoleId = tabs.openTabs[0].params?.consoleId as string;
    const console = useDatabaseStore.getState().consoleState(consoleId);
    expect(console.sql).toBe("select id from t");
    expect(console.results).toEqual([{ statement: "select id from t", result: rows, origin: "agent" }]);
  });
});

describe("what the console says about the agent's rows", () => {
  test("how many, and that a run refreshes them", () => {
    expect(agentResultNote({ rowCount: 25, truncated: false })).toBe("Agent's result · 25 rows — run to refresh");
    expect(agentResultNote({ rowCount: 1, truncated: false })).toBe("Agent's result · 1 row — run to refresh");
  });

  test("a cut result says where it was cut", () => {
    expect(agentResultNote({ rowCount: 200, truncated: true })).toBe(
      "Agent's result · truncated at 200 rows — run to refresh",
    );
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

describe("the connection a query card names", () => {
  const profiles = [profile("p1", "staging-db"), profile("p2", "prod")];

  test("by name when this device has it", () => {
    expect(connectionPhrase("p1", profiles)).toBe("on staging-db");
  });

  test("another connection when the loaded list does not have it", () => {
    expect(connectionPhrase("gone", profiles)).toBe("on another connection");
  });

  test("nothing when the result names none, or the list is not loaded", () => {
    expect(connectionPhrase(undefined, profiles)).toBeNull();
    expect(connectionPhrase("p1", [])).toBeNull();
  });
});

describe("what a switch here would lose", () => {
  test("nothing, when no transaction is open and nothing runs", () => {
    expect(liveProfileSelection.switchCost("p2")).toBeNull();
  });

  test("the open transaction and the consoles still running, by connection name", () => {
    useDatabaseStore.setState({
      txOpen: true,
      consoles: {
        a: { ...useDatabaseStore.getState().consoleState("a"), runningQuery: true },
        b: { ...useDatabaseStore.getState().consoleState("b"), runningQuery: false },
      },
    });
    try {
      expect(liveProfileSelection.switchCost("p2")).toEqual({
        from: "staging",
        to: "prod",
        transaction: true,
        runningQueries: 1,
      });
    } finally {
      useDatabaseStore.setState({ txOpen: false, consoles: {} });
    }
  });
});
