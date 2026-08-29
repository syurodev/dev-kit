import { describe, expect, it } from "bun:test";
import { resolveRefTarget } from "./ERDView";

const tables = [
  "crm_customers",
  "crm_leads",
  "his_billing_statements",
  "his_patients",
  "customers",
  "ba_user",
];

describe("resolveRefTarget", () => {
  it("prefers a table sharing the source table's prefix", () => {
    expect(resolveRefTarget("crm_leads", "customer_id", tables)).toBe("crm_customers");
  });

  it("falls back to the bare stem when no prefixed table exists", () => {
    expect(resolveRefTarget("audit_log", "customer_id", tables)).toBe("customers");
  });

  it("matches a singular prefixed table", () => {
    expect(resolveRefTarget("ba_session", "user_id", tables)).toBe("ba_user");
  });

  it("returns nothing rather than guessing when no table matches", () => {
    expect(resolveRefTarget("crm_leads", "external_id", tables)).toBe("");
  });

  it("never points a table at itself", () => {
    expect(resolveRefTarget("crm_customers", "customer_id", ["crm_customers"])).toBe("");
  });

  it("ignores the primary key itself", () => {
    expect(resolveRefTarget("crm_leads", "id", tables)).toBe("");
  });
});
