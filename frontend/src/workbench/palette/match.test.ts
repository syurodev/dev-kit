import { describe, expect, it } from "bun:test";
import { bestScore, matchScore, rank } from "./match";

describe("matchScore", () => {
  it("orders the tiers the way a launcher is expected to", () => {
    const scores = [
      matchScore("orders", "orders"), // the whole text
      matchScore("ord", "orders"), // its beginning
      matchScore("item", "order_items"), // the start of a word
      matchScore("der", "orders"), // anywhere inside
      matchScore("oitm", "order_items"), // in order, with gaps
    ];
    expect([...scores].sort((a, b) => b - a)).toEqual(scores);
    expect(scores.every((s) => s > 0)).toBe(true);
  });

  it("does not match what is not there", () => {
    expect(matchScore("xyz", "orders")).toBe(0);
    expect(matchScore("sredro", "orders")).toBe(0); // the letters, but not in order
    expect(matchScore("a", "")).toBe(0);
  });

  it("finds a word start at a camelCase step and after a separator", () => {
    const camel = matchScore("service", "OrderItemService");
    const snake = matchScore("service", "order_item_service");
    const inside = matchScore("ervice", "OrderItemService");
    expect(camel).toBeGreaterThan(inside);
    expect(snake).toBeGreaterThan(inside);
  });

  it("prefers the shorter text within a tier, never across tiers", () => {
    expect(matchScore("ord", "orders")).toBeGreaterThan(matchScore("ord", "order_audit_log_archive"));
    // A long prefix match still beats a short substring match.
    expect(matchScore("ord", "order_audit_log_archive")).toBeGreaterThan(matchScore("ord", "word"));
  });

  it("ignores case and surrounding space, and lets an empty query match everything", () => {
    expect(matchScore("  ORD ", "orders")).toBe(matchScore("ord", "orders"));
    expect(matchScore("", "anything")).toBeGreaterThan(0);
  });
});

describe("bestScore", () => {
  it("counts a match on the title above the same match on anything else", () => {
    const byTitle = bestScore("orders", "orders", ["public"]);
    const bySchema = bestScore("orders", "invoices", ["orders"]);
    expect(byTitle).toBeGreaterThan(bySchema);
    expect(bySchema).toBeGreaterThan(0);
  });
});

describe("rank", () => {
  const tables = ["order_items", "orders", "word_list", "customers", "reorder_rules"];
  const byName = (name: string) => ({ title: name });

  it("returns matches best first and leaves out the rest", () => {
    expect(rank("ord", tables, byName, 10)).toEqual(["orders", "order_items", "word_list", "reorder_rules"]);
  });

  it("limits, and keeps the given order for an empty query", () => {
    expect(rank("", tables, byName, 2)).toEqual(["order_items", "orders"]);
  });
});
