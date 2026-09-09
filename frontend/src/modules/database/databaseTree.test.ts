import { describe, expect, it } from "bun:test";
import { countTables, filterNamespace, matchesProfile } from "./databaseTree";

const namespace = {
  public: { ba_account: ["id"], ba_user: ["id"], billing_invoices: ["id"] },
  audit: { log_entries: ["id"] },
};

describe("filterNamespace", () => {
  it("returns the same object when there is no needle", () => {
    expect(filterNamespace(namespace, "")).toBe(namespace);
  });

  it("keeps only matching tables", () => {
    expect(filterNamespace(namespace, "ba_")).toEqual({
      public: { ba_account: ["id"], ba_user: ["id"] },
    });
  });

  it("drops a schema whose tables all fail — an empty schema row would read as 'nothing here'", () => {
    expect(Object.keys(filterNamespace(namespace, "log"))).toEqual(["audit"]);
  });

  it("matches case-insensitively", () => {
    expect(Object.keys(filterNamespace(namespace, "BILLING").public)).toEqual([
      "billing_invoices",
    ]);
  });
});

describe("matchesProfile", () => {
  const profile = { name: "chiro-prod", database: "chiro", host: "10.0.0.4" };

  it("matches an empty needle", () => {
    expect(matchesProfile(profile, "")).toBe(true);
  });

  it("matches on name, database and host — all three are how someone looks for a connection", () => {
    expect(matchesProfile(profile, "PROD")).toBe(true);
    expect(matchesProfile(profile, "chiro")).toBe(true);
    expect(matchesProfile(profile, "10.0.0")).toBe(true);
    expect(matchesProfile(profile, "nowhere")).toBe(false);
  });
});

describe("countTables", () => {
  it("counts across every schema", () => {
    expect(countTables(namespace)).toBe(4);
    expect(countTables({})).toBe(0);
  });
});
