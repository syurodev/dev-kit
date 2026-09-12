import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { ModuleSkeleton, railAlreadyShown } from "./ModuleSkeleton";
import { databaseModule } from "@/modules/database/manifest";

describe("ModuleSkeleton", () => {
  it("paints a full module shape, not one line of text", () => {
    const html = renderToString(<ModuleSkeleton />);
    expect(html).toContain('role="status"');
    expect(html.match(/data-slot="skeleton"/g)?.length ?? 0).toBeGreaterThanOrEqual(12);
    // Shape only — no visible copy; the aria-label carries the meaning.
    expect(html.replace(/<[^>]+>/g, "").trim()).toBe("");
  });
});

/**
 * The skeleton takes the rail column over when a module has none, and the
 * report this fixes is what happens when it gets that wrong: opening the
 * FIRST document in a module tore down the real rail — its scroll, its
 * expanded connection, its fetched list — and built it again a frame later,
 * because the skeleton asked only whether a DOCUMENT had registered one.
 */
describe("railAlreadyShown", () => {
  it("counts a rail a document registered", () => {
    expect(railAlreadyShown("a rail", undefined)).toBe(true);
  });

  // The case that regressed. Database registers nothing from its documents any
  // more — the rail is on the manifest — so "nothing registered" stopped
  // meaning "no rail".
  it("counts a rail the module declares on its manifest", () => {
    expect(railAlreadyShown(undefined, databaseModule.sidebar)).toBe(true);
  });

  it("is false only when neither exists, which is what the skeleton is for", () => {
    expect(railAlreadyShown(undefined, undefined)).toBe(false);
  });
});
