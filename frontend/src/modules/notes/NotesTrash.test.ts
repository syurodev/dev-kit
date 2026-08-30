import { describe, expect, it } from "bun:test";
import { daysUntilPurge } from "./NotesTrash";

const DAY = 86_400_000;
const now = Date.parse("2026-08-30T12:00:00Z");

describe("daysUntilPurge", () => {
  it("counts whole days left", () => {
    expect(daysUntilPurge(new Date(now + 12 * DAY).toISOString(), now)).toBe(12);
  });

  // Floor, not round: a note with 20 hours left has not reached its last day,
  // and "purges in 1 day" is the honest reading of that, not 0.
  it("floors a partial day rather than rounding it away", () => {
    expect(daysUntilPurge(new Date(now + DAY + 20 * 3_600_000).toISOString(), now)).toBe(1);
  });

  it("never reports a negative countdown for something already due", () => {
    expect(daysUntilPurge(new Date(now - 5 * DAY).toISOString(), now)).toBe(0);
  });

  it("survives a timestamp it cannot parse", () => {
    expect(daysUntilPurge("not a date", now)).toBe(0);
  });
});
