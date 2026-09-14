import { beforeEach, describe, expect, it, mock } from "bun:test";
import type {
  ConnectionProfile,
  CreateProfileInput,
  UpdateProfileOptionsInput,
  SessionRow,
  PlanNode,
  QueryPlan,
} from "./database";

describe("Database Bridge Adapter Types & Mappings", () => {
  it("verifies ConnectionProfile structure", () => {
    const profile: ConnectionProfile = {
      id: "prof_123",
      name: "Local Postgres",
      host: "localhost",
      port: 5432,
      database: "postgres",
      username: "postgres",
      sslMode: "disable",
      env: "dev",
      readOnly: false,
      engine: "postgres",
    };

    expect(profile.id).toBe("prof_123");
    expect(profile.engine).toBe("postgres");
    expect(profile.readOnly).toBe(false);
  });

  it("verifies CreateProfileInput with engine and UpdateProfileOptionsInput", () => {
    const input: CreateProfileInput = {
      name: "Prod MySQL",
      host: "db.internal",
      port: 3306,
      database: "app",
      username: "root",
      password: "secret",
      sslMode: "verify-full",
      env: "prod",
      engine: "mysql",
    };

    expect(input.engine).toBe("mysql");
    expect(input.port).toBe(3306);

    const updateOpts: UpdateProfileOptionsInput = {
      env: "staging",
      readOnly: true,
      sshProfileId: "ssh_abc",
    };

    expect(updateOpts.readOnly).toBe(true);
    expect(updateOpts.sshProfileId).toBe("ssh_abc");
  });

  it("verifies QueryPlan & PlanNode structure", () => {
    const rootNode: PlanNode = {
      operation: "Seq Scan on users",
      depth: 0,
      estimatedRows: 10,
      actualRows: 1000,
      selfMillis: 14.5,
      detail: "Filter: (active = true)",
      children: [],
    };

    const plan: QueryPlan = {
      nodes: [rootNode],
      raw: '{"Plan": {"Node Type": "Seq Scan"}}',
      analyzed: true,
    };

    expect(plan.nodes.length).toBe(1);
    expect(plan.analyzed).toBe(true);
    expect(plan.nodes[0].estimatedRows).toBe(10);
    expect(plan.nodes[0].actualRows).toBe(1000);
    // Ratio > 10x deviation check
    const ratio = plan.nodes[0].actualRows / Math.max(1, plan.nodes[0].estimatedRows);
    expect(ratio).toBeGreaterThan(10);
  });

  it("verifies SessionRow and Postgres blocked_by field", () => {
    const session: SessionRow = {
      pid: "1024",
      state: "active",
      duration: "00:01:23",
      user: "app_user",
      client: "10.0.0.5",
      waitEvent: "Lock:relation",
      blockedBy: ["998", "999"],
      query: "UPDATE accounts SET balance = balance - 100 WHERE id = 1;",
      application: "billing-worker",
      startedAt: "2026-08-29T20:00:00Z",
    };

    expect(session.blockedBy).toEqual(["998", "999"]);
    expect(session.blockedBy.length).toBe(2);
  });

});

// These run the adapter against a stubbed bridge. Asserting the shape of an
// object literal the test itself wrote proves nothing — it cannot fail unless
// the test is edited, which is how a green test hides a broken mapping.
describe("listActivity mapping", () => {
  let payload: unknown;

  beforeEach(() => {
    mock.module("../../bindings/devkit/internal/bridge", () => ({
      AppService: {
        DBListActivity: async () => payload,
        UIStateGet: async () => "",
        UIStateSet: async () => undefined,
      },
    }));
  });

  async function listActivityFresh() {
    const module = await import("./database");
    return module.listActivity("profile-1");
  }

  it("gives MySQL rows an empty blockedBy rather than undefined", async () => {
    // MySQL has no pg_blocking_pids equivalent, so the Go side omits the field.
    // A consumer that maps over blockedBy must not blow up on it.
    payload = [
      {
        pid: "42",
        state: "executing",
        duration: "00:00:05",
        user: "root",
        client: "localhost",
        wait_event: "Sending data",
        blocked_by: null,
        query: "SELECT * FROM orders;",
        application: "devkit",
        started_at: "2026-08-29T20:05:00Z",
      },
    ];
    const [session] = await listActivityFresh();
    expect(Array.isArray(session.blockedBy)).toBe(true);
    expect(session.blockedBy).toHaveLength(0);
    expect(() => session.blockedBy.map((b) => b)).not.toThrow();
  });

  it("keeps Postgres blockers and renames the wire fields", async () => {
    payload = [
      {
        pid: "4418",
        state: "waiting",
        duration: "00:03:51",
        user: "api",
        client: "10.0.2.44",
        wait_event: "Lock tuple",
        blocked_by: ["4412"],
        query: "UPDATE orders SET status = $1",
        application: "api",
        started_at: "2026-08-29T20:05:00Z",
      },
    ];
    const [session] = await listActivityFresh();
    expect(session.blockedBy).toEqual(["4412"]);
    expect(session.waitEvent).toBe("Lock tuple");
    expect(session.startedAt).toBe("2026-08-29T20:05:00Z");
  });

  it("survives a null list", async () => {
    payload = null;
    expect(await listActivityFresh()).toEqual([]);
  });
});
